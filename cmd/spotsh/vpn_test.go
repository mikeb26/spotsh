package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	iaws "github.com/mikeb26/spotsh/aws"
)

// The shell scripts run with a completely mocked PATH: these tests never invoke
// sudo, WireGuard, network commands, SSH, or AWS on the host.
func vpnScriptTest(t *testing.T, script string, args []string, scenario string) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "calls")
	mocks := map[string]string{
		"sudo": `#!/bin/bash
if [[ "$1" == "-v" ]]; then
    [[ "$SCENARIO" != sudo-fail ]]
    exit
fi
if [[ "$1" == sh ]]; then exit 0; fi
exec "$@"
`,
		"ip": `#!/bin/bash
echo "ip $*" >> "$CALL_LOG"
if [[ "$*" == "link show wg0" ]]; then
    [[ "$SCENARIO" == existing || "$SCENARIO" == teardown || "$SCENARIO" == foreign ]]
    exit
fi
if [[ "$*" == "-4 rule show" ]]; then
    if [[ "$SCENARIO" == teardown ]]; then
        echo '100: from all lookup main suppress_prefixlength 0'
        echo '200: not from all fwmark 0x4d2 lookup 2468'
    fi
    exit 0
fi
if [[ "$*" == "-4 route get "* ]]; then echo '192.0.2.1 via 192.0.2.254 dev enp1s0 src 192.0.2.2'; exit 0; fi
if [[ "$SCENARIO" == rule-fail && "$*" == "rule add table main suppress_prefixlength 0" ]]; then exit 1; fi
if [[ "$SCENARIO" == server-fail && "$*" == "addr replace "* ]]; then exit 1; fi
`,
		"wg": `#!/bin/bash
if [[ "$1" == show ]]; then echo "wg $1 $2 ${3:-}" >> "$CALL_LOG"; else echo "wg $1 $2" >> "$CALL_LOG"; fi
if [[ "$1" == pubkey || "$1" == show && "$3" == public-key ]]; then echo test-public-key; fi
if [[ "$1" == genkey ]]; then echo test-private-key; fi
if [[ "$1" == show && "$3" == fwmark ]]; then
    if [[ "$SCENARIO" == foreign ]]; then echo off; else echo 0x4d2; fi
fi
if [[ "$1" == show && "$3" == latest-handshakes ]]; then
    if [[ "$SCENARIO" == no-handshake ]]; then echo 'test-server-public-key 0'; else echo 'test-server-public-key 123'; fi
fi
`,
		"ping": `#!/bin/bash
echo ping >> "$CALL_LOG"
[[ "$SCENARIO" != no-handshake && "$SCENARIO" != no-ping ]]
`,
		"iptables": `#!/bin/bash
echo "iptables $*" >> "$CALL_LOG"
`,
	}
	for name, content := range mocks {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	path, err := filepath.Abs(script)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/bash", append([]string{path}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin", "CALL_LOG="+log, "SCENARIO="+scenario, "SSH_CLIENT=192.0.2.1 12345 22")
	out, err := cmd.CombinedOutput()
	calls, _ := os.ReadFile(log)
	return string(out), string(calls), err
}

func TestVpnClientSetup(t *testing.T) {
	args := []string{"test-server-public-key", "192.0.2.5", "private-key-file"}
	for _, tt := range []struct {
		name, scenario, output string
		fail                   bool
	}{
		{"success", "success", "tunnel server responded", false},
		{"no handshake", "no-handshake", "no handshake", true},
		{"handshake without ping", "no-ping", "handshake observed", true},
		{"partial route failure", "rule-fail", "command failed", true},
		{"existing interface", "existing", "wg0 already exists", true},
		{"sudo failure", "sudo-fail", "checking existing interface", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, calls, err := vpnScriptTest(t, "setupVpnClient.sh", vpnScriptArgs(true, args...), tt.scenario)
			if (err != nil) != tt.fail || !strings.Contains(out, tt.output) {
				t.Fatalf("error=%v output=%s calls=%s", err, out, calls)
			}
			if strings.Contains(out, "test-server-public-key") || strings.Contains(out, "private-key-file") {
				t.Fatalf("key details in output: %s", out)
			}
			if tt.scenario == "success" && strings.Index(calls, "ping") > strings.Index(calls, "ip rule add") {
				t.Fatalf("routing changed before connectivity check: %s", calls)
			}
			if tt.scenario == "no-handshake" || tt.scenario == "no-ping" {
				if strings.Contains(calls, "ip rule add") || !strings.Contains(calls, "ip link delete wg0") {
					t.Fatalf("invalid rollback: %s", calls)
				}
			}
			if tt.scenario == "rule-fail" && (!strings.Contains(calls, "ip rule delete not fwmark") || !strings.Contains(calls, "ip link delete wg0") || strings.Contains(calls, "ip rule delete table main")) {
				t.Fatalf("invalid rollback: %s", calls)
			}
			if tt.scenario == "existing" && strings.Contains(calls, "ip link delete") {
				t.Fatalf("deleted existing interface: %s", calls)
			}
		})
	}
}

func TestVpnScriptVerbosity(t *testing.T) {
	clientArgs := []string{"test-server-public-key", "192.0.2.5", "private-key-file"}
	for _, tt := range []struct {
		script, scenario, progress, failure string
		args                                []string
	}{
		{"setupVpnClient.sh", "success", "tunnel server responded", "", clientArgs},
		{"setupVpnClient.sh", "no-handshake", "checking tunnel handshake", "no handshake", clientArgs},
		{"setupVpnClient.sh", "rule-fail", "creating wg0", "command failed", clientArgs},
		{"setupVpnServer.sh", "success", "outbound interface", "", []string{"client-public-key", "public-key-file"}},
		{"setupVpnServer.sh", "server-fail", "configuring WireGuard", "command failed", []string{"client-public-key", "public-key-file"}},
		{"teardownVpnClient.sh", "teardown", "cleanup complete", "", nil},
		{"teardownVpnClient.sh", "foreign", "checking local routing", "refusing to modify", nil},
	} {
		t.Run(tt.script+"/"+tt.scenario, func(t *testing.T) {
			quiet, quietCalls, quietErr := vpnScriptTest(t, tt.script, vpnScriptArgs(false, tt.args...), tt.scenario)
			verbose, verboseCalls, verboseErr := vpnScriptTest(t, tt.script, vpnScriptArgs(true, tt.args...), tt.scenario)
			if (quietErr != nil) != (tt.failure != "") || (verboseErr != nil) != (tt.failure != "") {
				t.Fatalf("quiet err=%v verbose err=%v; outputs: %s / %s", quietErr, verboseErr, quiet, verbose)
			}
			if strings.Contains(quiet, tt.progress) || !strings.Contains(verbose, tt.progress) {
				t.Fatalf("progress: quiet=%q verbose=%q", quiet, verbose)
			}
			if tt.failure == "" && quiet != "" {
				t.Fatalf("unexpected quiet output: %q", quiet)
			}
			if tt.failure != "" && (!strings.Contains(quiet, tt.failure) || !strings.Contains(verbose, tt.failure)) {
				t.Fatalf("missing error: quiet=%q verbose=%q", quiet, verbose)
			}
			if tt.scenario == "no-handshake" {
				if strings.Contains(quiet, "WireGuard transfer:") || !strings.Contains(verbose, "WireGuard transfer:") || strings.Contains(quietCalls, "wg show wg0 transfer") {
					t.Fatalf("transfer diagnostics: quiet=%q (%s), verbose=%q (%s)", quiet, quietCalls, verbose, verboseCalls)
				}
			}
			for _, output := range []string{quiet, verbose} {
				if strings.Contains(output, "test-server-public-key") || strings.Contains(output, "private-key-file") {
					t.Fatalf("key details in output: %s", output)
				}
			}
		})
	}
}

func TestParseVpnArgs(t *testing.T) {
	for _, tt := range []struct {
		args             []string
		instance, action string
		verbose, fail    bool
	}{
		{[]string{"start"}, "", "start", false, false},
		{[]string{"-v", "--instance-id", "i-test", "start"}, "i-test", "start", true, false},
		{[]string{"start", "-v", "--instance-id", "i-test"}, "i-test", "start", true, false},
		{[]string{"--instance-id", "i-test", "STOP", "-v"}, "i-test", "stop", true, false},
		{[]string{"-v", "stop", "-v=false"}, "", "stop", false, false},
		{nil, "", "", false, true},
		{[]string{"-v", "invalid"}, "", "", true, true},
		{[]string{"stop", "extra"}, "", "stop", false, true},
		{[]string{"stop", "--bad"}, "", "stop", false, true},
	} {
		instance, action, verbose, err := parseVpnArgs(tt.args, io.Discard)
		if (err != nil) != tt.fail || instance != tt.instance || action != tt.action || verbose != tt.verbose {
			t.Errorf("%v: got %q %q %v %v", tt.args, instance, action, verbose, err)
		}
	}
}

func TestVpnScriptsArgumentValidation(t *testing.T) {
	for _, script := range []string{"setupVpnClient.sh", "setupVpnServer.sh"} {
		for _, args := range [][]string{nil, {"one"}, {"one", "two", "three", "four"}} {
			out, calls, err := vpnScriptTest(t, script, args, "success")
			if err == nil || !strings.Contains(out, "Usage:") || calls != "" {
				t.Fatalf("%s %v: err=%v output=%s calls=%s", script, args, err, out, calls)
			}
		}
	}
}

func TestVpnServerSetup(t *testing.T) {
	for _, scenario := range []string{"success", "existing", "server-fail"} {
		out, calls, err := vpnScriptTest(t, "setupVpnServer.sh", []string{"-v", "client-public-key", "public-key-file"}, scenario)
		if (err != nil) != (scenario == "server-fail") {
			t.Fatalf("%s: %v output=%s calls=%s", scenario, err, out, calls)
		}
		if scenario == "success" && (!strings.Contains(out, "outbound interface enp1s0") || !strings.Contains(calls, "iptables -t nat -C POSTROUTING -o enp1s0")) {
			t.Fatalf("server diagnostics: %s %s", out, calls)
		}
		if scenario == "existing" && strings.Contains(calls, "ip link add") {
			t.Fatalf("existing interface recreated: %s", calls)
		}
		if scenario == "server-fail" && (!strings.Contains(out, "command failed") || !strings.Contains(calls, "ip link delete wg0")) {
			t.Fatalf("missing failure diagnostics: %s", out)
		}
	}
}

func TestVpnTeardown(t *testing.T) {
	out, calls, err := vpnScriptTest(t, "teardownVpnClient.sh", []string{"-v"}, "teardown")
	if err != nil || !strings.Contains(out, "cleanup complete") || !strings.Contains(calls, "ip rule delete table main") || !strings.Contains(calls, "ip rule delete not fwmark") || !strings.Contains(calls, "ip link delete wg0") || strings.Contains(calls, "flush") {
		t.Fatalf("err=%v output=%s calls=%s", err, out, calls)
	}
	out, calls, err = vpnScriptTest(t, "teardownVpnClient.sh", nil, "foreign")
	if err == nil || !strings.Contains(out, "refusing to modify") || strings.Contains(calls, "ip rule delete") || strings.Contains(calls, "ip link delete") {
		t.Fatalf("foreign wg0: err=%v output=%s calls=%s", err, out, calls)
	}
}

type cancellingHTTPClient struct{}

func (cancellingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestVpnTagUpdateHonorsContext(t *testing.T) {
	cfg := aws.Config{Region: "us-east-1", Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
	}), HTTPClient: cancellingHTTPClient{}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := iaws.UpdateTagContext(ctx, cfg, "i-test", "test", "true")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
