# MACVZ

This project is inspired by and a rewrite of lima-vm.

The major difference is macvz uses macOS new [Virtualization API](https://developer.apple.com/documentation/virtualization?language=objc) instead of QEMU for spinning up VM's.

References used in the project,
1. Go Binding for MAC Virtualization API - https://github.com/Code-Hex/vz

# Requirements
- Higher or equal to macOS monterey (12.2)
- Golang

# Wiki
Check out Github Wiki page [Why use virtualization framework](https://github.com/balajiv113/macvz/wiki/Why-use-macOS-virtualization-framework-%3F) to understand more on advantages of Virtualization framework

## Summary of filesystem performance with colima

The following table contains result summary of some different workloads tested against macvz and colima

### Summary for IOPS (Input Ouput Per Second)
| Workload            | Summary                            | macvz   | colima |
|---------------------|------------------------------------|---------|--------|
| Sequential Reads    | macvz handles 8x higher operations | 620K    | 77K    |
| Random Reads        | macvz handles 3x higher operations | 82K     | 25K    |
| Random Reads/Writes | macvz handles 3x higher operations | 37K/12K | 14K/4K |
| Sequential writes   | macvz performs almost equally      | 37K     | 38K    |
| Random writes       | macvz performs almost equally      | 22K     | 30K    |

### Summary for Bandwidth (Maximum amount of data transmitted)
| Workload            | Summary                       | macvz      | colima    |
|---------------------|-------------------------------|------------|-----------|
| Sequential Reads    | macvz handles 8x more data    | 2500MB     | 306MB     |
| Random Reads        | macvz handles 3x more data    | 320MB      | 98MB      |
| Random Reads/Writes | macvz handles 3x more data    | 140MB/50MB | 60MB/20MB |
| Sequential writes   | macvz performs almost equally | 145MB      | 150MB     |
| Random writes       | macvz performs almost equally | 90MB       | 110MB     |

# Features
- [x] Start, stop, shell access for multiple VMs
- [x] Filesystem mounting using virtfs
- [x] Working docker example
- [x] Port binding (Initial support present, Needs testing !!!)

# Getting Started
## Installation via Homebrew
- Run `brew install mac-vz/tap/macvz` to install macvz

## Installation via source
- Run `make all` to compile and build binary
- Run `make install` to install the binary to /usr/local

## Using macvz as a alternate for Docker Desktop
To start a Docker VM, run the following command
```
macvz start https://raw.githubusercontent.com/mac-vz/macvz/main/examples/docker.yaml
```

Execute the following command in macOS host to update docker.sock location
```
export DOCKER_HOST=unix://${HOME}/.macvz/docker/sock/docker.sock
```

That's it !! 


## Other macvz commands

To get shell access to a running VM,
```
macvz shell docker
```

To stop a running VM,
```
macvz stop docker
```

## Project Status
⚠️ The project is still in early stage development and may introduce breaking changes.
