# Spotsh Skills

This file is a practical workflow cookbook for humans and AI agents using
[`spotsh`](README.md). Each skill describes a task that `spotsh` can perform,
when to use it, and the commands to run.

`spotsh` creates real AWS resources. Be cost-aware, prefer explicit cleanup, and
terminate disposable instances when finished:

```bash
spotsh terminate
```

## Skill: Launch or connect to a disposable cloud shell

Use `spotsh` when you need a clean temporary Linux machine in AWS with SSH
access.

```bash
spotsh
```

With no command, `spotsh` connects to an existing spot shell instance. If no
spot shell exists, it launches one first and then SSHs into it.

Good for:

- clean Linux environments;
- short-lived build or test boxes;
- remote compute when a laptop is underpowered;
- disposable execution environments for coding agents;
- AWS-region-local debugging or network egress.

Clean up when finished:

```bash
spotsh terminate
```

## Skill: Launch a specific Linux distribution

Use `--os` to choose the operating system for a new spot shell.

```bash
spotsh launch --os ubuntu24.04
spotsh ssh
```

Common values include:

- `amzn2023` for Amazon Linux 2023;
- `ubuntu24.04` for Ubuntu 24.04 LTS;
- `debian12` for Debian GNU/Linux 12.

Run `spotsh help` for the full current list.

## Skill: Run initialization commands on first boot

Use `--initcmd` when the instance should install packages or perform setup as it
starts.

Ubuntu example:

```bash
spotsh launch --os ubuntu24.04 --initcmd 'sudo apt-get update && sudo apt-get install -y git build-essential'
spotsh ssh
```

Amazon Linux example:

```bash
spotsh launch --os amzn2023 --initcmd 'sudo dnf -y install git make gcc'
spotsh ssh
```

This is useful for repeatable dev boxes, CI-like test machines, and AI agent
execution environments.

## Skill: Find inexpensive Spot capacity

Use `spotsh price` to compare Spot prices and placement scores before launching.

```bash
spotsh --region all price --sort-vcpu
```

Limit the comparison to candidate instance types:

```bash
spotsh price --types c7i.large,c7a.large,c8i.large,c8a.large --all-azs
```

Use this before expensive or long-running work to choose a region, availability
zone, or instance family with better availability and lower cost.

## Skill: Launch with selected instance types, AZs, and maximum price

Use launch flags to control where and how much capacity `spotsh` may request.

```bash
spotsh launch \
  --os ubuntu24.04 \
  --types c7i.large,c7a.large,c8i.large,c8a.large \
  --azs us-east-2a,us-east-2b \
  --spotprice 0.05
```

This is useful when you already know the cheapest or most reliable capacity
pools for your workload.

## Skill: Copy files to and from the spot shell

Use `spotsh scp` for artifact transfer. The special `{s}` placeholder is
replaced with the selected spot shell's `user@public-ip`.

Copy a local file to the instance:

```bash
spotsh scp -- ./artifact.tar.gz {s}:/tmp/artifact.tar.gz
```

Copy a directory from the instance:

```bash
spotsh scp -- -rp {s}:/tmp/results ./results
```

Copy a repository bundle to the instance:

```bash
tar czf repo.tar.gz .
spotsh scp -- repo.tar.gz {s}:/tmp/repo.tar.gz
```

## Skill: Run a one-off remote command

Use `spotsh ssh -- '<command>'` when you want the spot shell to run a command
without opening an interactive session.

```bash
spotsh ssh -- 'uname -a'
```

Example: unpack a copied repository and run tests.

```bash
spotsh ssh -- 'mkdir -p /tmp/repo && tar xzf /tmp/repo.tar.gz -C /tmp/repo && cd /tmp/repo && make test'
```

This pattern is especially useful for automation and AI agents.

## Skill: Give an AI coding agent a disposable remote machine

A typical AI-agent workflow is:

1. launch a clean Linux instance;
2. install build dependencies;
3. copy or clone the project;
4. run tests or experiments;
5. terminate the instance.

Example:

```bash
spotsh launch --os ubuntu24.04 --initcmd 'sudo apt-get update && sudo apt-get install -y git build-essential'
tar czf repo.tar.gz .
spotsh scp -- repo.tar.gz {s}:/tmp/repo.tar.gz
spotsh ssh -- 'mkdir -p /tmp/repo && tar xzf /tmp/repo.tar.gz -C /tmp/repo && cd /tmp/repo && make test'
spotsh terminate
```

Agent safety tips:

- terminate the instance at the end of the task;
- avoid copying secrets unless they are required;
- prefer least-privilege IAM roles;
- use `spotsh info` to inspect running resources before and after work.

## Skill: Inspect spot shells and related resources

Use `spotsh info` to see running spot shell instances and related resources.

```bash
spotsh info
```

Show all supported resource categories:

```bash
spotsh info --all
```

Inspect specific categories:

```bash
spotsh info --instances --keys --vpcs --images
```

Use this before cleanup, debugging, or cost review.

## Skill: Attach an IAM role to a spot shell

Use `--role` when the shell needs AWS permissions from an EC2 instance profile.

```bash
spotsh launch --role my-instance-profile-role-name
```

Security guidance:

- prefer narrowly scoped roles;
- avoid long-lived static AWS credentials on the instance;
- do not grant broad administrative permissions unless absolutely necessary.

## Skill: Launch from a custom AMI

Use a custom AMI when you want a preconfigured image with tools, caches, or
project dependencies already installed.

```bash
spotsh launch --ami ami-0123456789abcdef0 --user ubuntu
```

Or launch by AMI name:

```bash
spotsh launch --ami-name my-devbox-ami --user ec2-user
```

Specify `--user` unless `spotsh` can infer the SSH username from the AMI name.

## Skill: Save a configured shell box as an AMI

After configuring a spot shell, create an AMI for future launches.

```bash
spotsh image --name my-spotsh-devbox --desc 'Configured spotsh development box'
```

Remember that AMIs and EBS snapshots may continue to incur storage cost until
deleted.

## Skill: Use a spot shell as a temporary VPN endpoint

VPN mode uses WireGuard and is currently intended for Amazon Linux 2023 spot
shell instances.

```bash
spotsh launch --os amzn2023
spotsh vpn start
```

Stop VPN mode when finished:

```bash
spotsh vpn stop
```

Use this when you need temporary AWS-region egress from your local machine.

## Skill: Configure default preferences

Run the interactive configuration helper to set defaults such as region,
instance types, OS, or key settings.

```bash
spotsh config
```

Preferences are stored under `~/.config/spotsh/`.

## Skill: Clean up disposable resources

Terminate the active spot shell when you are finished.

```bash
spotsh terminate
```

Before and after cleanup, inspect state:

```bash
spotsh info
```

If you created AMIs, snapshots, custom security group rules, or other AWS
resources outside the normal `spotsh terminate` flow, review and delete anything
that should not persist.
