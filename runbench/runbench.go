// Command runbench is a custom benchmark tool for profiling kopia and exporting metrics for
// storage time series database.
//
// It runs provided benchmark scenario scripts and captures selected kopia invocation capturing
// CPU/RAM metrics, prometheus metrics, repository size and emits InfluxDB-formatted
// time series data.
//
// Usage: runbench [--flags] scenario1.sh ... scenarioN.sh
//
// Each scenario file is a simple bash script that prepares the test, it must contain exactly
// one line starting with:
//
//   [ -z "COLLECT_METRICS" ] &&
//
// This prefix prevents the command from running as part of bash script and allows the tool
// to parse it and run separately with metric collection.
//
// The tool relies on build information embedded in each Kopia binary (which relies on Go 1.18 or later)
//
// For each scenario the tool generates one output file:
// <outputDir>/<scenario>/<gitTime>-<gitHash>.line
//
// This can be imported into InfluxDB using `influx write --file=<path>`
//
package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	stdlog "log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/logging"
	"github.com/google/shlex"
	"github.com/pkg/errors"
	"github.com/shirou/gopsutil/v3/process"
)

var log = stdlog.Default()

// this is a sepcial marker that must prefix the single (last) line in the script
// this serves two purposes - prevents bash from running the command and indicates which
// command to collect metrics for.
const collectMetricsMarker = `[ -z "COLLECT_METRICS" ] && `

var (
	kopiaExe            = flag.String("kopia-exe", os.ExpandEnv("$HOME/go/bin/kopia"), "Path to kopia")
	compareExe          = flag.String("compare-to-exe", "", "Path to executable to compare against")
	runTags             = flag.String("run-tags", "", "Comma-separated list of tags to attach to measurements")
	repoPath            = flag.String("repo-path", "/tmp/kopia-test-repo", "Path to repository directory")
	outputDir           = flag.String("output-dir", "/tmp/kopia-benchmark-outputs", "Output directory")
	timestamp           = flag.Int64("timestamp", 0, "Override benchmark timestamp")
	force               = flag.Bool("force", false, "Force run even if output already exists")
	minDuration         = flag.Duration("min-duration", 2*time.Minute, "Repeat scenarios until they run for a given minum time")
	minRepeat           = flag.Int("min-repeat", 2, "Repeat scenarios a given minum number of times")
	disableCloudLogging = flag.Bool("disable-cloud-logging", false, "Disable cloud logging")
)

var (
	gitTime     time.Time
	gitRevision string
	gitModified bool
)

type sample struct {
	ts                time.Time
	ram               float64 // MiB
	cpu               float64
	prometheusMetrics []byte
}

type runResult struct {
	duration time.Duration

	repoSizeBytes int64
	numRepoFiles  int

	// prometheus metrics
	go_memstats_alloc_bytes_total float64
	go_memstats_mallocs_total     float64

	samples []*sample
}

func summarizeDir(dir string, numFiles *int, totalSize *int64) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return errors.Wrap(err, "error reading dir")
	}

	for _, e := range entries {
		if e.IsDir() {
			if err := summarizeDir(filepath.Join(dir, e.Name()), numFiles, totalSize); err != nil {
				return err
			}

			continue
		}

		info, err := e.Info()
		if err != nil {
			return errors.Wrap(err, "error getting info")
		}

		*totalSize += info.Size()
		*numFiles++
	}

	return nil
}

func parsePrometheusCounters(b []byte) map[string]float64 {
	res := map[string]float64{}

	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		l := s.Text()

		if strings.HasPrefix(l, "#") {
			continue
		}

		parts := strings.Split(l, " ")
		if len(parts) != 2 {
			continue
		}

		name := parts[0]
		value, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			continue
		}

		res[name] = value
	}

	return res
}

func runCommandAndSample(ctx context.Context, c *exec.Cmd, timeOffset time.Duration) (*runResult, error) {
	t0 := time.Now()

	err := c.Start()
	if err != nil {
		return nil, errors.Wrap(err, "unable to start")
	}

	var (
		dur    time.Duration
		runErr error
		wg     sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		runErr = c.Wait()
		dur = time.Since(t0)
		wg.Done()
	}()

	proc, err := process.NewProcessWithContext(ctx, int32(c.Process.Pid))
	if err != nil {
		return nil, errors.Wrap(err, "unable to attach to process")
	}

	var samples []*sample

	for {
		s := &sample{
			ts: time.Now().Add(timeOffset),
		}

		mi, err := proc.MemoryInfoWithContext(ctx)
		if err != nil {
			break
		}

		cpuPercent, err := proc.CPUPercentWithContext(ctx)
		if err != nil {
			break
		}

		s.cpu = cpuPercent
		s.ram = float64(mi.RSS) / (1 << 20)

		resp, err := http.Get("http://localhost:6666/metrics")
		if err == nil {
			s.prometheusMetrics, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
		}

		samples = append(samples, s)

		time.Sleep(100 * time.Millisecond)
	}

	wg.Wait()

	if len(samples) == 0 {
		return nil, errors.Errorf("no samples")
	}

	var numFiles int
	var totalSize int64

	if *repoPath != "" {
		if err := summarizeDir(*repoPath, &numFiles, &totalSize); err != nil {
			return nil, errors.Wrap(err, "error summarizing repository")
		}
	}

	rr := &runResult{
		samples:       samples,
		duration:      dur,
		numRepoFiles:  numFiles,
		repoSizeBytes: totalSize,
	}

	for _, s := range samples {
		counters := parsePrometheusCounters(s.prometheusMetrics)

		if v := counters["go_memstats_alloc_bytes_total"]; v > 0 {
			rr.go_memstats_alloc_bytes_total = v
		}

		if v := counters["go_memstats_mallocs_total"]; v > 0 {
			rr.go_memstats_mallocs_total = v
		}
	}

	return rr, runErr
}

func runKopia(ctx context.Context, timeOffset time.Duration, exe string, args ...string) (*runResult, error) {
	c := exec.CommandContext(ctx, exe, append([]string{"--metrics-listen-addr=:6666"}, args...)...)
	c.Env = append(append([]string(nil), os.Environ()...),
		"KOPIA_EXE="+exe,
		"REPO_PATH="+*repoPath,
	)

	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	return runCommandAndSample(ctx, c, timeOffset)
}

func runPrepare(ctx context.Context, scenarioFile string) error {
	c := exec.Command(scenarioFile)
	c.Env = append(append([]string(nil), os.Environ()...),
		"KOPIA_EXE="+*kopiaExe,
		"REPO_PATH="+*repoPath,
	)

	out, err := c.CombinedOutput()

	return errors.Wrapf(err, "failed with %s", out)
}

type runSummary struct {
	avgCPU float64
	maxCPU float64
	avgRAM float64
	maxRAM float64

	avgRepoSize    float64
	avgFileCount   float64
	avgDuration    float64
	avgHeapObjects float64
	avgHeapBytes   float64
}

func summarizeSamples(rrs []*runResult) runSummary {
	var (
		totalCPU         float64
		totalRAM         float64
		totalDuration    float64
		totalFiles       float64
		totalRepoSize    float64
		totalHeapObjects float64
		totalHeapBytes   float64
		maxCPU           float64
		maxRAM           float64
		cnt              int
	)

	for _, rr := range rrs {
		totalDuration += rr.duration.Seconds()
		totalFiles += float64(rr.numRepoFiles)
		totalRepoSize += float64(rr.repoSizeBytes)
		totalHeapObjects += float64(rr.go_memstats_mallocs_total)
		totalHeapBytes += float64(rr.go_memstats_alloc_bytes_total)

		for _, s := range rr.samples {
			totalCPU += s.cpu
			totalRAM += float64(s.ram)

			if s.cpu > maxCPU {
				maxCPU = s.cpu
			}

			if s.ram > maxRAM {
				maxRAM = s.ram
			}

			cnt++
		}
	}

	return runSummary{
		avgCPU: totalCPU / float64(cnt),
		maxCPU: maxCPU,
		avgRAM: totalRAM / float64(cnt),
		maxRAM: maxRAM,

		avgRepoSize:    totalRepoSize / float64(len(rrs)),
		avgFileCount:   totalFiles / float64(len(rrs)),
		avgDuration:    totalDuration / float64(len(rrs)),
		avgHeapObjects: totalHeapObjects / float64(len(rrs)),
		avgHeapBytes:   totalHeapBytes / float64(len(rrs)),
	}
}

func compareValues(current, baseline float64) string {
	v := current / baseline

	var percentageChange string
	if v > 1 {
		percentageChange = fmt.Sprintf("+%.1f %%", 100*(v-1))
	} else if v < 1 {
		percentageChange = fmt.Sprintf("-%.1f %%", 100*(1-v))
	} else {
		percentageChange = "0%"
	}

	return fmt.Sprintf(" current:%.1f baseline:%.1f change:%v", current, baseline, percentageChange)
}

func compareSamples(f io.Writer, scen string, rrs, baseline []*runResult) {
	summ := summarizeSamples(rrs)
	summ2 := summarizeSamples(baseline)

	//fmt.Fprintf(f, "duration:,repo_size=%v,num_files=%v %v\n",
	fmt.Fprintf(f, "duration:%v\n", compareValues(summ.avgDuration, summ2.avgDuration))
	fmt.Fprintf(f, "repo_size:%v\n", compareValues(summ.avgRepoSize, summ2.avgRepoSize))
	fmt.Fprintf(f, "num_files:%v\n", compareValues(summ.avgFileCount, summ2.avgFileCount))

	fmt.Fprintf(f, "avg_heap_objects:%v\n", compareValues(summ.avgHeapObjects, summ2.avgHeapObjects))
	fmt.Fprintf(f, "avg_heap_bytes:%v\n", compareValues(summ.avgHeapBytes, summ2.avgHeapBytes))

	fmt.Fprintf(f, "avg_ram:%v\n", compareValues(summ.avgRAM, summ2.avgRAM))
	fmt.Fprintf(f, "max_ram:%v\n", compareValues(summ.maxRAM, summ2.maxRAM))

	fmt.Fprintf(f, "avg_cpu:%v\n", compareValues(summ.avgCPU, summ2.avgCPU))
	fmt.Fprintf(f, "max_cpu:%v\n", compareValues(summ.maxCPU, summ2.maxCPU))
}

func logSamples(f io.Writer, scen string, rrs []*runResult) {
	summ := summarizeSamples(rrs)

	// log.Printf("dur: %v CPU avg:%.1f max:%.1f RAM avg:%.1f max:%.1f", rr.duration, totalCPU/float64(len(rr.samples)), maxCPU, float64(totalRAM)/((1<<20)*float64(len(rr.samples))), float64(maxRAM)/float64((1<<20)))

	tags := strings.Join([]string{
		fmt.Sprintf("rev=%v", gitRevision),
		fmt.Sprintf("mod=%v", gitModified),
		fmt.Sprintf("gitTime=%v", gitTime.Unix()),
		fmt.Sprintf("scenario=%v", scen),
	}, ",")

	if *runTags != "" {
		tags += "," + *runTags
	}

	fmt.Fprintf(f, "process_summary,%v duration=%.1f,repo_size=%v,num_files=%v %v\n",
		tags,
		summ.avgDuration,
		summ.avgRepoSize,
		summ.avgFileCount,
		gitTime.UnixNano(),
	)

	fmt.Fprintf(f, "process_heap_summary,%v avg_heap_objects=%v,avg_heap_bytes=%v %v\n",
		tags,
		summ.avgHeapObjects,
		summ.avgHeapBytes,
		gitTime.UnixNano(),
	)
	fmt.Fprintf(f, "process_ram_summary,%v avg_ram_rss=%v,max_ram_rss=%v %v\n",
		tags,
		summ.avgRAM,
		summ.maxRAM,
		gitTime.UnixNano(),
	)

	fmt.Fprintf(f, "process_cpu_summary,%v avg_cpu_percent=%v,max_cpu_percent=%v %v\n",
		tags,
		summ.avgCPU,
		summ.maxCPU,
		gitTime.UnixNano(),
	)
}

func parseScenario(fname string) (string, []string, error) {
	f, err := os.Open(fname)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()

	var lines []string

	s := bufio.NewScanner(f)
	for s.Scan() {
		if strings.HasPrefix(s.Text(), collectMetricsMarker) {
			lines = append(lines, strings.TrimPrefix(s.Text(), collectMetricsMarker))
		}
	}

	if len(lines) != 1 {
		return "", nil, errors.Errorf("expected %q to have exactly one line, got %v", fname, len(lines))
	}

	expanded := strings.ReplaceAll(lines[0], "$KOPIA_EXE", *kopiaExe)
	expanded = strings.ReplaceAll(expanded, "$REPO_PATH", *repoPath)
	expanded = os.ExpandEnv(expanded)

	parts, err := shlex.Split(expanded)
	if err != nil {
		return "", nil, errors.Wrap(err, "unable to split")
	}

	return parts[0], parts[1:], nil
}

func failOnError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func parseBuildInfo() {
	c := exec.Command("go", "version", "-m", *kopiaExe)
	o, err := c.Output()
	failOnError(errors.Wrap(err, "unable to run go version"))
	s := bufio.NewScanner(bytes.NewReader(o))
	for s.Scan() {
		fields := strings.Fields(s.Text())

		if len(fields) != 2 && fields[0] != "build" {
			continue
		}

		p := strings.SplitN(fields[1], "=", 2)
		if len(p) < 2 {
			continue
		}

		key := p[0]
		val := p[1]

		switch key {
		case "vcs.time":
			t, err := time.Parse(time.RFC3339, val)
			failOnError(err)
			gitTime = t
		case "vcs.revision":
			gitRevision = val
		case "vcs.modified":
			gitModified = val == "true"
		}
	}

	if *timestamp != 0 {
		gitTime = time.Unix(*timestamp, 0)
	}
}

func runMultiple(ctx context.Context, scenFile string, timeOffset time.Duration, exe string, args []string) []*runResult {
	var (
		runs          []*runResult
		totalDuration time.Duration
		totalCount    int
	)

	for totalDuration < *minDuration || totalCount < *minRepeat {
		log.Printf("Run #%v (%v), total duration %v", totalCount+1, exe, totalDuration)
		log.Printf("  preparing...")
		failOnError(runPrepare(ctx, scenFile))
		log.Printf("  running...")
		t0 := time.Now()
		rr, err := runKopia(ctx, timeOffset, exe, args...)
		failOnError(err)

		if totalCount > 0 {
			// discard first result as a warmup
			runs = append(runs, rr)
		}

		totalDuration += time.Since(t0)
		totalCount++
		log.Printf("  completed in %v dir size: %v allocated bytes %v allocated objects: %v", rr.duration, rr.repoSizeBytes, int64(rr.go_memstats_alloc_bytes_total), int64(rr.go_memstats_mallocs_total))
	}

	return runs
}

func main() {
	flag.Parse()

	ctx := context.Background()

	// when running on GCP publish logs to Cloud Logging
	if cloudProject, _ := metadata.ProjectID(); cloudProject != "" && !*disableCloudLogging {
		client, err := logging.NewClient(ctx, cloudProject)
		if err != nil {
			log.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		logName := "runbench"
		log = client.Logger(logName).StandardLogger(logging.Info)
	}

	parseBuildInfo()

	for _, scenFile := range flag.Args() {
		scen := strings.TrimSuffix(filepath.Base(scenFile), ".sh")

		outputFile := filepath.Join(*outputDir, scen, gitTime.UTC().Format("2006-01-02_150405")+"-"+gitRevision+".line")

		log.Printf("Running benchmark:")
		log.Printf("   scenario %q", scenFile)
		log.Printf("   executable %q", *kopiaExe)
		log.Printf("   revision %q (%v) modified:%v", gitRevision, gitTime, gitModified)
		log.Printf("   output file %q", outputFile)

		if _, err := os.Stat(outputFile); err == nil && !*force && *compareExe == "" {
			log.Println("output already exists and --force not passed")
			continue
		}

		exe, args, err := parseScenario(scenFile)
		failOnError(err)

		// compute offset such that now + offset == gitTime
		// so that runs for a given time are clustered around it.
		timeOffset := time.Until(gitTime)

		runs := runMultiple(ctx, scenFile, timeOffset, exe, args)
		if *compareExe != "" {
			compareRuns := runMultiple(ctx, scenFile, timeOffset, *compareExe, args)

			compareSamples(os.Stdout, scen, runs, compareRuns)

			continue
		}

		if outputFile != "" {
			failOnError(os.MkdirAll(filepath.Dir(outputFile), 0700))
			f, err := os.Create(outputFile)
			failOnError(err)
			defer f.Close()

			logSamples(f, scen, runs)
		} else {
			logSamples(os.Stdout, scen, runs)
		}
	}
}
