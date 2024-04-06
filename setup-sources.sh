#!/bin/bash
set -ex

export PATH=/usr/lib/go-1.21/bin:$PATH
MAKEMANYFILES=~/go/bin/makemanyfiles
RUNBENCH=~/go/bin/runbench

setup_packages() {
	sudo apt install -y axel golang-1.21-go
}

setup_tools() {
	(cd makemanyfiles && go build -o $MAKEMANYFILES .)
	(cd runbench && go build -o $RUNBENCH .)
}

download_isos() {
	if [ ! -d ~/backup-sources/isos ]; then
		mkdir -p ~/backup-sources/isos
		echo $(date) downloading ISOs...
		pushd ~/backup-sources/isos
		axel http://old-releases.ubuntu.com/releases/20.04.3/ubuntu-20.04-desktop-amd64.iso
		axel http://old-releases.ubuntu.com/releases/20.04.3/ubuntu-20.04.1-desktop-amd64.iso
		axel http://old-releases.ubuntu.com/releases/20.04.3/ubuntu-20.04.2-desktop-amd64.iso
		axel http://old-releases.ubuntu.com/releases/20.04.3/ubuntu-20.04.3-desktop-amd64.iso
		axel http://img.cs.montana.edu/linux/debian/11/amd/debian-11.0.0-amd64-netinst.iso
		popd
		echo $(date) finished ISOs...
	else
  		echo ISOs already downloaded...
	fi
}

download_linux() {
	if [ ! -d ~/backup-sources/linux ]; then
		echo $(date) downloading linux...
		mkdir ~/backup-sources/linux
		curl -L https://kernel.org/pub/linux/kernel/v5.x/linux-5.14.8.tar.xz | tar Jx -C ~/backup-sources/linux
		echo $(date) finished downloading linux...
	else
		echo Linux already downloaded...
	fi
}

download_vmdisk_sparse() {
	if [ ! -d ~/backup-sources/vmdisk-sparse ]; then
		echo $(date) downloading vmdisk-sparse...
		mkdir -p ~/backup-sources/vmdisk-sparse
		gsutil cat gs://kopia-backup-sources/vmdisk-sparse.tar | tar x -C ~/backup-sources/vmdisk-sparse/
		echo $(date) finished downloading vmdisk-sparse...
	else
		echo vmdisk-sparse already downloaded...
	fi
}

# create one giant directory with 100K files of 10000 bytes each (1GB total)
# each file has 1000 random bytes repeated 10 times
write_100k_flat_compressible() {
	if [ ! -d ~/backup-sources/100k-flat-compressible ]; then
		$MAKEMANYFILES --output-dir ~/backup-sources/100k-flat-compressible --num-files 100000 --file-length=1000 --file-data-repeat=10
	else
		echo ~/backup-sources/100k-flat-compressible already exists
	fi
}

# create one giant directory with roughly 150K files which are a delta on top of 100K
# with some files deleted, some rewritten, some added
write_150k_flat_compressible() {
	if [ ! -d ~/backup-sources/150k-flat-compressible ]; then
		# copy 100k files from the other source
	    cp -a ~/backup-sources/100k-flat-compressible ~/backup-sources/150k-flat-compressible 
		# delete 1/256th of the files
		find ~/backup-sources/150k-flat-compressible -name '??00*' -exec rm {} \;
		# rewrite 50k of files so they have the same contents but different metadata
		$MAKEMANYFILES --output-dir ~/backup-sources/150k-flat-compressible --num-files 50000 --file-length=1000 --file-data-repeat=10
		# create 50k of additional files with different seed
		$MAKEMANYFILES --output-dir ~/backup-sources/150k-flat-compressible --num-files 50000 --file-length=1000 --file-data-repeat=10 --seed 7654321
	else
		echo ~/backup-sources/150k-flat-compressible already exists
	fi
}


# 1000 files, each 1000 x 1000 bytes repeated
write_1k_flat_large_compressible() {
	if [ ! -d ~/backup-sources/1k-flat-large-compressible ]; then
		$MAKEMANYFILES --output-dir ~/backup-sources/1k-flat-large-compressible --num-files 1000 --file-length=1000 --file-data-repeat=1000
	else
		echo ~/backup-sources/1k-flat-large-compressible already exists
	fi
}

# create one giant directory with 1M files of 1000 bytes each (1GB total)
write_1mfiles_flat() {
	if [ ! -d ~/backup-sources/1mfiles-flat ]; then
		$MAKEMANYFILES --output-dir ~/backup-sources/1mfiles-flat --num-files 1000000 --file-length=1000 --file-data-repeat=10
	else
		echo ~/backup-sources/1mfiles-flat already exists
	fi
}

write_1_5mfiles_flat() {
	if [ ! -d ~/backup-sources/1_5mfiles-flat ]; then
	    cp -a ~/backup-sources/1mfiles-flat ~/backup-sources/1_5mfiles-flat 
		# delete 1/256th of the files
		find ~/backup-sources/1_5mfiles-flat -name '??00*' -exec rm {} \;
		# rewrite 500k of files so they have the same contents but different metadata
		$MAKEMANYFILES --output-dir ~/backup-sources/1_5mfiles-flat --num-files 500000 --file-length=1000 --file-data-repeat=10
		# create 500k of additional files with different seed
		$MAKEMANYFILES --output-dir ~/backup-sources/1_5mfiles-flat --num-files 500000 --file-length=1000 --file-data-repeat=10 --seed 7654321
	else
		echo ~/backup-sources/1_5mfiles-flat already exists
	fi
}

# create 256 directories with a total of 1M files (approx 3900 files per dir)
write_1mfiles_sharded_256() {
	if [ ! -d ~/backup-sources/1mfiles-sharded_256 ]; then
		$MAKEMANYFILES --output-dir ~/backup-sources/1mfiles-sharded_256 --num-files 1000000 --file-length=1000 --shard1=2
	else
		echo ~/backup-sources/1mfiles-sharded_256 already exists
	fi
}

# create 4096=16x256 directories with a total of 100000 files (approx 25 files per dir)
write_100k_files_sharded_16_256() {
	if [ ! -d ~/backup-sources/100kfiles-sharded_16_256 ]; then
		$MAKEMANYFILES --output-dir ~/backup-sources/100kfiles-sharded_16_256 --num-files 100000 --file-length=1000 --shard1=2 --shard2=2
	else
		echo ~/backup-sources/100kfiles-sharded_16_256 already exists
	fi
}

setup_packages
setup_tools
#download_vmdisk_sparse
download_isos
download_linux
write_1k_flat_large_compressible
write_100k_flat_compressible
write_150k_flat_compressible
write_1mfiles_flat
write_1_5mfiles_flat
write_1mfiles_sharded_256
write_100k_files_sharded_16_256

mkdir -p binaries
