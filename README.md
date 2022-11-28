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
spotsh help
spotsh - Spot Shell

  Utility for creating/terminating/ssh'ing to an EC2 spot instance

Usage:
  spotsh [<command>]

Available Commands:
  help                 This help screen
  info                 List spot shell instances, security groups, and
                       available key pairs
  launch               Launch a new spot shell instance
  ssh [<flags>]        ssh to an existing spot shell instance
  terminate [<flags>]  Terminate an existing spot shell instance
  version              Print spotsh's version string

By default when command is not specified spotsh will attempt to ssh to
an existing spot shell instance. If a spot shell instance does not
exist, it will be created.

Flags:
  --instance-id <EC2_instance_id>
```

## Contributing
Pull requests are welcome at https://github.com/mikeb26/spotsh

For major changes, please open an issue first to discuss what you
would like to change.

## License
[AGPL3](https://www.gnu.org/licenses/agpl-3.0.en.html)
