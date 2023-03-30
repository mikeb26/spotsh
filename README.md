# Spot Shell (spotsh)
Utility for launching &amp; connecting to a shell on a spot EC2 instance

## Building

```bash
make
make test
```

## Installing

```bash
mkdir -p $HOME/bin
SPOTSH=$(curl -s https://api.github.com/repos/mikeb26/spotsh/releases/latest | grep browser_download_url | cut -f2,3 -d: | tr -d \")
wget $SPOTSH
chmod 755 spotsh
mv spotsh $HOME/bin
# add $HOME/bin to your $PATH if not already present
```

## Usage

```bash
spotsh - Spot Shell

  Utility for creating/terminating/ssh'ing to an EC2 spot instance

Usage:
  spotsh [<GLOBALFLAGS>] [<command>]

Available Commands:
  config                         Set spotsh default preferences
  help                           This help screen
  info                           List spot shell instances, security
                                 groups, and available key pairs
  launch [<LAUNCHFLAGS>]         Launch a new spot shell instance
  price [<PRICEFLAGS>]           Display spot prices
  ssh [<SSHFLAGS>]               ssh to an existing spot shell instance
  scp [<SSHFLAGS>] -- <SCP_ARGS> scp to/from an existing spot shell
                                 instance
  terminate [<SSHFLAGS>]         Terminate an existing spot shell
                                 instance
  upgrade                        Upgrade to the latest version of spotsh
  version                        Print spotsh's version string
  vpn [<SSHFLAGS>] start         Start VPN session to a spot shell instance
  vpn [<SSHFLAGS>] stop          Teardown VPN session to a spot shell instance

By default when command is not specified spotsh will attempt to ssh to
an existing spot shell instance. If a spot shell instance does not
exist, it will be created.

SSHFLAGS:                                       | DEFAULT
  --instance-id <EC2_instance_id>               | existing spotsh
                                                  instance if running

LAUNCHFLAGS:                                    | DEFAULT
  --os <OPERATING_SYSTEM>                       | amzn2
  --ami <ami_id>                                | latest amzn2 AMI id
  --ami-name <ami_name>                         | ignored
  --key <keypair_name>                          | spotsh.<your_aws_region>
  
  --sgid <security_group_id>                    | default VPC's default
                                                  security group
  --role <iam_role_name>                        | none
  --initcmd <initial_cmd_to_run>                | none
  --types <instance_type>[,<instance_type>...]  | c5a.large,c5.large,\
                                                  c6i.large,c6a.large
  --spotprice <maximum_spot_price>              | 0.08 which represents
                                                  $0.08/hour
  --user <username_to_ssh_as>                   | os's default user

GLOBALFLAGS:                                    | DEFAULT
  --region <aws_region>                         | same default as set by
                                                  'aws configure'
  --region all (price cmd only)                 | n/a

PRICEFLAGS:                                     | DEFAULT
  --types <instance_type>[,<instance_type>...]  | c5a.large,c5.large,\
                                                  c6i.large,c6a.large

OPERATING_SYSTEM:
  When launching an instance the operating system to launch with can
  be specified with the --os flag. The current list of supported
  operating systems is below:

    amzn2023    - Amazon Linux 2023 (standard)
    amzn2023min - Amazon Linux 2023 (minimal)
    amzn2       - Amazon Linux 2
    ubuntu22.04 - Ubuntu 22.04 LTS

SCP_ARGS:
  With 1 exception SCP_ARGS are passed directly to scp. See SCP(1) for
  more detail. The exception is user@host replacement. spotsh defines
  a special variable, {s}, which can be used in any SCP_ARGS. Spot
  shell will replace all instances of {s} with the spot instance's
  user and public ip address before passing the argument to scp. For
  example, to copy a local file /tmp/foo to the spot instance:
  
    $ spotsh scp -- /tmp/foo {s}:/tmp/foo

  To recursively copy the contents of /var/log from the spot instance
  locally:
  
    $ spotsh scp -- -rp {s}:/var/log /tmp/spotlogs
```

## Contributing
Pull requests are welcome at https://github.com/mikeb26/spotsh

For major changes, please open an issue first to discuss what you
would like to change.

## License
[AGPL3](https://www.gnu.org/licenses/agpl-3.0.en.html)
