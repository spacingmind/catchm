package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/summarize"
	"github.com/spacingmind/catchm/internal/summarize/eval"
)

const goldDir = "../../internal/summarize/eval/testdata"

type fixedSummarizer struct {
	a   summarize.Analysis
	err error
}

func (f fixedSummarizer) Summarize(context.Context, summarize.Input) (summarize.Analysis, error) {
	return f.a, f.err
}

// runCLI runs `catchm eval args` against an empty CATCHM_HOME, handing
// back the config the summarizer was built from.
func runCLI(t *testing.T, sum summarize.Summarizer, args ...string) (code int, stdout, stderr string, cfg *config.Config) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CATCHM_HOME", home)
	var out, errb bytes.Buffer
	code = runEval(context.Background(), args, &out, &errb,
		func(c *config.Config, _ summarize.CloudAuth) (summarize.Summarizer, error) {
			cfg = c
			return sum, nil
		})
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("eval wrote to CATCHM_HOME: %v", entries)
	}
	return code, out.String(), errb.String(), cfg
}

func TestEvalJSON(t *testing.T) {
	code, stdout, stderr, cfg := runCLI(t, fixedSummarizer{a: summarize.Analysis{Summary: "s"}},
		"--dir", goldDir, "--provider", "ollama", "--model", "tiny:1b", "--json")
	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if cfg.Summary.OllamaModel != "tiny:1b" {
		t.Errorf("model override not applied: %q", cfg.Summary.OllamaModel)
	}
	var out eval.Output
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if out.Provider != "ollama" || out.Model != "tiny:1b" || out.Evaluated != 5 || out.Aggregate == nil {
		t.Fatalf("output = %+v", out)
	}
	// An empty analysis finds nothing: recall 0, but no hallucinations.
	if r := out.Aggregate; r.DecisionRecall == nil || *r.DecisionRecall != 0 || *r.DecisionPrecision != 1 {
		t.Errorf("aggregate = %+v", r)
	}
}

func TestEvalTable(t *testing.T) {
	code, stdout, _, cfg := runCLI(t, fixedSummarizer{a: summarize.Analysis{Summary: "s"}},
		"--dir", goldDir, "--provider", "openai", "--model", "m1")
	if code != 0 || cfg.Summary.OpenAI.Model != "m1" {
		t.Fatalf("exit %d, openai model %q", code, cfg.Summary.OpenAI.Model)
	}
	for _, s := range []string{"provider: openai  model: m1", "a-en-product-standup", "e-vi-social-catchup", "MEAN (5/5)"} {
		if !strings.Contains(stdout, s) {
			t.Errorf("table missing %q:\n%s", s, stdout)
		}
	}
}

func TestEvalNothingEvaluated(t *testing.T) {
	down := fixedSummarizer{err: &summarize.Error{Kind: summarize.KindNotReady, Message: "Ollama isn't running"}}
	code, stdout, stderr, _ := runCLI(t, down, "--dir", goldDir)
	if code != 1 || !strings.Contains(stderr, "no file could be evaluated") || !strings.Contains(stdout, "Ollama isn't running") {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

func TestEvalCloudUnsupported(t *testing.T) {
	code, _, stderr, cfg := runCLI(t, fixedSummarizer{}, "--dir", goldDir, "--provider", "catchm-cloud")
	if code != 1 || cfg != nil || !strings.Contains(stderr, "not supported from the CLI yet") {
		t.Fatalf("exit %d, summarizer built: %v, stderr: %s", code, cfg != nil, stderr)
	}
}

func TestEvalUsage(t *testing.T) {
	for _, args := range [][]string{{"--provider", "gpt"}, {"extra"}, {"--nope"}} {
		if code, _, _, _ := runCLI(t, fixedSummarizer{}, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if code, _, stderr, _ := runCLI(t, fixedSummarizer{}, "--dir", filepath.Join(t.TempDir(), "missing")); code != 1 || stderr == "" {
		t.Errorf("missing dir: exit %d, stderr %q", code, stderr)
	}
}
