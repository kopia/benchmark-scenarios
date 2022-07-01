#!/bin/bash
set -e

sudo apt-get update && sudo apt-get install -y axel golang make

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
		./makemanyfiles-linux-amd64 --output-dir ~/backup-sources/100k-flat-compressible --num-files 100000 --file-length=1000 --file-data-repeat=10
	else
		echo ~/backup-sources/100k-flat-compressible already exists
	fi
}


# 1000 files, each 1000 x 1000 bytes repeated
write_1k_flat_large_compressible() {
	if [ ! -d ~/backup-sources/1k-flat-large-compressible ]; then
		./makemanyfiles-linux-amd64 --output-dir ~/backup-sources/1k-flat-large-compressible --num-files 1000 --file-length=1000 --file-data-repeat=1000
	else
		echo ~/backup-sources/1k-flat-large-compressible already exists
	fi
}

# create one giant directory with 1M files of 1000 bytes each (1GB total)
write_1mfiles_flat() {
	if [ ! -d ~/backup-sources/1mfiles-flat ]; then
		./makemanyfiles-linux-amd64 --output-dir ~/backup-sources/1mfiles-flat --num-files 1000000 --file-length=1000
	else
		echo ~/backup-sources/1mfiles-flat already exists
	fi
}

# create 256 directories with a total of 1M files (approx 3900 files per dir)
write_1mfiles_sharded_256() {
	if [ ! -d ~/backup-sources/1mfiles-sharded_256 ]; then
		./makemanyfiles-linux-amd64 --output-dir ~/backup-sources/1mfiles-sharded_256 --num-files 1000000 --file-length=1000 --shard1=2
	else
		echo ~/backup-sources/1mfiles-sharded_256 already exists
	fi
}

# create 4096=16x256 directories with a total of 100000 files (approx 25 files per dir)
write_100k_files_sharded_16_256() {
	if [ ! -d ~/backup-sources/100kfiles-sharded_16_256 ]; then
		./makemanyfiles-linux-amd64 --output-dir ~/backup-sources/100kfiles-sharded_16_256 --num-files 100000 --file-length=1000 --shard1=2 --shard2=2
	else
		echo ~/backup-sources/100kfiles-sharded_16_256 already exists
	fi
}

download_vmdisk_sparse
download_isos
download_linux
write_1k_flat_large_compressible
write_100k_flat_compressible
write_1mfiles_flat
write_1mfiles_sharded_256
write_100k_files_sharded_16_256
