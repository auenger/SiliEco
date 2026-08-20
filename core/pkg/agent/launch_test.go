package agent

import (
	"io"
	"log/slog"
	"reflect"
	"testing"
)

func TestLaunchPrefixPrecedesEveryProviderInvocation(t *testing.T) {
	t.Parallel()
	for _, provider := range SupportedTypes {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			cfg := Config{LaunchPrefix: []string{"wrapper-subcommand", "--profile", "tenant-a"}}
			backend, err := New(provider, cfg)
			if err != nil {
				t.Fatalf("New(%q): %v", provider, err)
			}
			value := reflect.ValueOf(backend).Elem().FieldByName("cfg")
			stored := value.FieldByName("LaunchPrefix")
			got := make([]string, stored.Len())
			for i := range got {
				got[i] = stored.Index(i).String()
			}
			want := FilterLaunchPrefix(provider, cfg.LaunchPrefix, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("stored launch prefix = %#v, want %#v", got, want)
			}
			argv := Command{Prefix: got}.Argv("provider-subcommand", "--model", "selected")
			wantArgv := append(append([]string{}, want...), "provider-subcommand", "--model", "selected")
			if !reflect.DeepEqual(argv, wantArgv) {
				t.Fatalf("argv = %#v, want %#v", argv, wantArgv)
			}
		})
	}
}

func TestFilterLaunchPrefixBlocksProtocolCriticalFlagAndItsValue(t *testing.T) {
	t.Parallel()
	got := FilterLaunchPrefix("claude", []string{"start", "tenant-a", "--output-format", "text", "--model", "opus"}, nil)
	want := []string{"start", "tenant-a", "--model", "opus"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FilterLaunchPrefix() = %#v, want %#v", got, want)
	}
}

func TestCommandArgvDoesNotAliasInputs(t *testing.T) {
	t.Parallel()
	prefix := []string{"start", "tenant-a"}
	cmd := NewCommand("wrapper", prefix)
	argv := cmd.Argv("acp", "serve")
	prefix[0] = "mutated"
	argv[0] = "also-mutated"
	if cmd.Prefix[0] != "start" {
		t.Fatalf("command prefix was aliased: %#v", cmd.Prefix)
	}
}
