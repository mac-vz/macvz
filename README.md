# MACVZ

This project is inspired by and a rewrite of lima-vm.

The major difference is macvz uses macOS new [Virtualization API](https://developer.apple.com/documentation/virtualization?language=objc) instead of QEMU for spinning up VM's.

References used in the project,
1. Go Binding for MAC Virtualization API - https://github.com/Code-Hex/vz

# Requirements
- Higher or equal to macOS monterey (12.2)
- Golang

# Features
- [x] Start, stop, shell access for multiple VMs
- [x] Filesystem mounting using virtfs
- [x] Working docker example
- [x] Port binding (Initial support present, Needs testing !!!)

# Getting Started
## Installation
- Run `make all` to compile and build binary
- Run `make install` to install the binary to /usr/local

## Using macvz as a alternate for Docker Desktop
To start a Docker VM, run the following command
```
macvz start https://raw.githubusercontent.com/balajiv113/macvz/main/examples/docker.yaml
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
