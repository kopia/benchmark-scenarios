package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/hkdf"
)

var (
	outputDir      = flag.String("output-dir", "", "")
	seed           = flag.Int64("seed", 123, "Seed")
	numFiles       = flag.Int("num-files", 0, "Number of files")
	fileLength     = flag.Int("file-length", 0, "Length of each file")
	shard1         = flag.Int("shard1", 0, "First level shard length")
	shard2         = flag.Int("shard2", 0, "Second level shard length")
	shard3         = flag.Int("shard3", 0, "Third level shard length")
	parallel       = flag.Int("parallel", 4, "Parallel")
	fileDataRepeat = flag.Int("file-data-repeat", 1, "Repeat contents of each file")
)

var counter = new(int32)

func main() {
	flag.Parse()

	if *outputDir == "" {
		log.Fatal("missing --output-dir")
	}

	t0 := time.Now()

	os.Mkdir(*outputDir, 0o700)

	var wg sync.WaitGroup

	for w := 0; w < *parallel; w++ {
		wg.Add(1)

		w := w

		go func() {
			defer wg.Done()

			for i := 0; i < *numFiles; i++ {
				if i%*parallel != w {
					continue
				}

				h := sha256.New()
				fmt.Fprintf(h, "%v.%v", *seed, i)
				fname := hex.EncodeToString(h.Sum(nil))
				outDir := *outputDir

				if s := *shard1; s > 0 {
					outDir = filepath.Join(outDir, fname[0:s])
					fname = fname[s:]

					os.Mkdir(outDir, 0o700)
				}

				if s := *shard2; s > 0 {
					outDir = filepath.Join(outDir, fname[0:s])
					fname = fname[s:]

					os.Mkdir(outDir, 0o700)
				}

				if s := *shard3; s > 0 {
					outDir = filepath.Join(outDir, fname[0:s])
					fname = fname[s:]

					os.Mkdir(outDir, 0o700)
				}

				if err := writeFile(filepath.Join(outDir, fname), i); err != nil {
					log.Fatal(err)
				}

				if c := atomic.AddInt32(counter, 1); c%1000 == 0 && c < int32(*numFiles) {
					log.Printf("wrote %v/%v files", c, *numFiles)
				}
			}
		}()
	}

	wg.Wait()
	log.Printf("wrote %v files of %v x %v bytes to %v in %v", atomic.LoadInt32(counter), *fileDataRepeat, *fileLength, *outputDir, time.Since(t0))
}

func writeFile(fname string, n int) error {
	f, err := os.Create(fname)
	if err != nil {
		return err
	}

	defer f.Close()

	for i := 0; i < *fileDataRepeat; i++ {
		r := hkdf.New(sha256.New, []byte(fmt.Sprintf("%v", n)), []byte(fmt.Sprintf("%v", *seed)), nil)
		_, err = io.CopyN(f, r, int64(*fileLength))
	}

	return err
}
