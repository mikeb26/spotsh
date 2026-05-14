# Spot Shell (`spotsh`)

[![Release](https://img.shields.io/github/v/release/mikeb26/spotsh)](https://github.com/mikeb26/spotsh/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/mikeb26/spotsh.svg)](https://pkg.go.dev/github.com/mikeb26/spotsh)
[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)

**Launch a cheap, disposable AWS EC2 Spot shell box and SSH into it with one command.**

`spotsh` is a small Go CLI for people who want temporary Linux compute without
clicking through the AWS console. It launches an EC2 Spot instance, manages the
SSH key and instance metadata it needs, opens SSH ingress in security
groups when necessary, and connects you to the instance from your terminal.

Use it when you want to:

- get a clean remote Linux environment quickly;
- run builds, tests, or experiments away from your laptop;
- give a coding or AI agent a disposable cloud machine;
- work from AWS-region-local network egress;
- copy artifacts to/from a throwaway instance;
- create an AMI from a configured shell box;
- temporarily route traffic through an EC2-backed WireGuard VPN.

## Quick start

```bash
spotsh
```

With no command, `spotsh` connects to an existing spot shell instance. If none
exists, it launches one first and then SSHs into it. It will
automatically create ssh keys and open ports in the security group
allowing access from your host if needed.

When you are done:

```bash
spotsh terminate
```

Check current Spot prices and placement scores:

```bash
spotsh price --region all --sort-vcpu
```

Launch Ubuntu instead of the default Amazon Linux 2023 image:

```bash
spotsh launch --os ubuntu24.04
spotsh ssh
```

Copy files with the special `{s}` placeholder for `user@public-ip`:

```bash
spotsh scp -- ./artifact.tar.gz {s}:/tmp/artifact.tar.gz
spotsh scp -- -rp {s}:/tmp/results ./results
```

## Why `spotsh`?

`spotsh` automates the boring parts of disposable EC2 shells:

- launches **one-time EC2 Spot** capacity via EC2 Fleet;
- tries multiple instance types and availability zones to find launchable cheap
  capacity;
- looks up current Spot pricing and placement scores;
- discovers the latest supported OS images;
- creates or reuses an EC2 key pair;
- tracks instances with `spotsh.*` tags;
- uses terminate-on-shutdown behavior for disposable instances;
- supports `ssh`, `scp`, `terminate`, `price`, `info`, `image`, and `vpn`
  workflows from one CLI.

## Cost and safety

`spotsh` creates real AWS resources. By default it uses EC2 Spot instances,
one-time Spot requests, and a maximum Spot price of `$0.08/hour`, but your AWS
account is still responsible for EC2, EBS, data transfer, AMI, and related
charges.

Recommended cleanup commands:

```bash
spotsh info
spotsh terminate
```

If you create an AMI with `spotsh image`, remember that AMIs and snapshots can
continue to incur storage cost until deleted.

## Installation

### Download the latest release

```bash
mkdir -p "$HOME/bin"
curl -L https://github.com/mikeb26/spotsh/releases/latest/download/spotsh -o "$HOME/bin/spotsh"
chmod 755 "$HOME/bin/spotsh"
# Add $HOME/bin to your PATH if it is not already there.
```

### Install with Go

```bash
go install github.com/mikeb26/spotsh/cmd/spotsh@latest
```

### Build from source

```bash
git clone https://github.com/mikeb26/spotsh.git
cd spotsh
make
```

## Requirements

- AWS credentials available through the standard AWS SDK mechanisms
  (`~/.aws/config`, environment variables, SSO, IAM role, etc.).
- An AWS region configured, or pass `--region <aws_region>`.
- Local `ssh` and `scp` binaries.
- For `spotsh vpn`: local WireGuard tools and an Amazon Linux 2023 spot shell.

Run the interactive configuration helper to set defaults:

```bash
spotsh config
```

Preferences are stored under `~/.config/spotsh/`.

## Common workflows

### Launch or connect

```bash
spotsh
```

Equivalent workflow:

```bash
spotsh launch
spotsh ssh
```

### Choose OS, instance types, availability zones, and max price

```bash
spotsh launch \
  --os ubuntu24.04 \
  --types c7i.large,c7a.large,c8i.large,c8a.large \
  --azs us-east-2a,us-east-2b \
  --spotprice 0.05
```

Supported OS values:

| Value | OS |
| --- | --- |
| `amzn2023` | Amazon Linux 2023 |
| `amzn2023min` | Amazon Linux 2023 minimal |
| `amzn2` | Amazon Linux 2 |
| `ubuntu22.04` | Ubuntu 22.04 LTS |
| `ubuntu24.04` | Ubuntu 24.04 LTS |
| `ubuntu26.04` | Ubuntu 26.04 LTS |
| `debian12` | Debian GNU/Linux 12 |
| `debian13` | Debian GNU/Linux 13 |

### Run setup on first boot

`--initcmd` is passed as EC2 user data for the new instance:

```bash
spotsh launch --os ubuntu24.04 --initcmd 'sudo apt-get update && sudo apt-get install -y git make gcc'
spotsh ssh
```

### Attach an IAM role

```bash
spotsh launch --role my-instance-profile-role-name
```

### Use a custom AMI

When launching from a custom AMI, specify the SSH user unless `spotsh` can infer
it from the AMI name:

```bash
spotsh launch --ami ami-0123456789abcdef0 --user ubuntu
# or
spotsh launch --ami-name my-devbox-ami --user ec2-user
```

### Inspect running shells and related resources

```bash
spotsh info
spotsh info --all
spotsh info --instances --keys --vpcs --images
```

### Find cheap regions and capacity pools

```bash
spotsh price --region all --sort-vcpu
spotsh price --types c7i.large,c7a.large,c8i.large,c8a.large --all-azs
```

### Copy files

`spotsh scp` passes arguments directly to `scp`, except that `{s}` is replaced
with the selected spot shell's `user@public-ip`.

```bash
spotsh scp -- ./local-file {s}:/tmp/local-file
spotsh scp -- -rp {s}:/var/log ./spotlogs
```

### Save a configured shell as an AMI

```bash
spotsh image --name my-spotsh-devbox --desc 'Configured spotsh development box'
```

### Start and stop VPN mode

VPN mode uses WireGuard and is currently supported for Amazon Linux 2023 spot
shell instances.

```bash
spotsh launch --os amzn2023
spotsh vpn start
spotsh vpn stop
```

## AI agent workflows

`spotsh` works well as a disposable cloud execution environment for coding
agents and automation. Typical pattern:

```bash
spotsh launch --os ubuntu24.04 --initcmd 'sudo apt-get update && sudo apt-get install -y git build-essential'
spotsh scp -- ./repo.tar.gz {s}:/tmp/repo.tar.gz
spotsh ssh -- 'mkdir -p /tmp/repo && tar xzf /tmp/repo.tar.gz -C /tmp/repo && cd /tmp/repo && make test'
spotsh terminate
```

Agent-friendly documentation files in this repository:

- [`AGENTS.md`](AGENTS.md): repository-specific guidance for coding agents.

Useful follow-up docs for agent discoverability would be a `SKILLS.md` workflow
cookbook and an `llms.txt` project summary.

## Command reference

```text
spotsh - Spot Shell

  Utility for creating/terminating/ssh'ing to an EC2 spot instance

Usage:
  spotsh [<GLOBALFLAGS>] [<command>]

Available Commands:
  config                         Set spotsh default preferences
  help                           This help screen
  info [<INFOFLAGS>]             List spot shell instances, security
                                 groups, and/or available key pairs
  launch [<LAUNCHFLAGS>]         Launch a new spot shell instance
  price [<PRICEFLAGS>]           Display spot prices and placement scores
  ssh [<SSHFLAGS>]               ssh to an existing spot shell instance
  scp [<SSHFLAGS>] -- <SCP_ARGS> scp to/from an existing spot shell
                                 instance
  terminate [<SSHFLAGS>]         Terminate an existing spot shell
                                 instance
  upgrade                        Upgrade to the latest version of spotsh
  version                        Print spotsh's version string
  vpn [<SSHFLAGS>] start         Start VPN session to a spot shell instance
  vpn [<SSHFLAGS>] stop          Teardown VPN session to a spot shell instance
  image [<IMAGEFLAGS>]           Create an AMI from an existing spot shell instance

By default when command is not specified spotsh will attempt to ssh to
an existing spot shell instance. If a spot shell instance does not
exist, it will be created.
```

For the full embedded CLI help, run:

```bash
spotsh help
```

## Development

This repository expects vendored dependencies for Makefile-based builds.

```bash
make vendor
make build
```

Safe local validation that does not call AWS APIs:

```bash
go test .
```

The broader test targets include AWS integration tests under `./aws`. They may
query or modify real AWS resources, including EC2 key pairs, VPC/security group
rules, SSM parameters, Spot instances, and AMIs. Do not run the full suite unless
you have AWS credentials configured and understand the possible cost and side
effects.

## How is this different from AWS CloudShell?

AWS CloudShell is browser-based and AWS-managed. `spotsh` gives you a real EC2
instance with SSH, SCP, selectable instance types, selectable Linux images,
optional IAM role attachment, AMI creation, and optional VPN behavior.

## How is this different from launching EC2 manually?

Manual EC2 launches are flexible but repetitive. `spotsh` packages the common
throwaway-shell workflow into a terminal command: key pair handling, latest AMI
lookup, Spot pricing, placement, launch, SSH, SCP, firewall changes,
tagging, and cleanup.

## Contributing

Pull requests are welcome at <https://github.com/mikeb26/spotsh>.

For major changes, please open an issue first to discuss what you would like to
change.

When changing command behavior, keep CLI UX stable and update both `README.md`
and `cmd/spotsh/help.txt` as needed.

## License

[AGPL-3.0](https://www.gnu.org/licenses/agpl-3.0.en.html)
