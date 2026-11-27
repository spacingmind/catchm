package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/summarize"
	"github.com/spacingmind/catchm/internal/summarize/eval"
)

// newSummarizerFunc builds a Summarizer from config; summarize.New in
// production, a stub in tests.
type newSummarizerFunc func(cfg *config.Config, cloud summarize.CloudAuth) (summarize.Summarizer, error)

// runEval implements `catchm eval`: it scores the configured summary
// provider on a directory of gold meetings and prints the result. It
// only reads the config and the gold files; nothing is written. The exit
// code is 0 when at least one file was evaluated, 1 when none was, and
// 2 for bad usage.
func runEval(ctx context.Context, args []string, stdout, stderr io.Writer, newSum newSummarizerFunc) int {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: catchm eval [--dir DIR] [--provider ollama|openai|catchm-cloud] [--model MODEL] [--json]")
		fs.PrintDefaults()
	}
	dir := fs.String("dir", "internal/summarize/eval/testdata", "directory of gold *.json files")
	provider := fs.String("provider", "", "summary provider (default: summary.provider from the config)")
	model := fs.String("model", "", "model name (default: the provider's model from the config)")
	asJSON := fs.Bool("json", false, "print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "catchm eval: load config: %v\n", err)
		return 1
	}
	switch *provider {
	case "", summarize.ProviderOllama, summarize.ProviderOpenAI, summarize.ProviderCloud:
	default:
		fmt.Fprintf(stderr, "catchm eval: unknown provider %q (want ollama, openai or catchm-cloud)\n", *provider)
		return 2
	}
	if *provider != "" {
		cfg.Summary.Provider = *provider
	}
	p := summarize.ProviderOf(cfg)
	if p == summarize.ProviderCloud {
		fmt.Fprintln(stderr, "catchm eval: the catchm-cloud provider needs the app's logged-in session, so it is not supported from the CLI yet; skipping.")
		return 1
	}
	if *model != "" {
		switch p {
		case summarize.ProviderOllama:
			cfg.Summary.OllamaModel = *model
		case summarize.ProviderOpenAI:
			cfg.LLM.StrongModel = *model
		}
	}

	golds, err := eval.Load(*dir)
	if err != nil {
		fmt.Fprintf(stderr, "catchm eval: %v\n", err)
		return 1
	}
	sum, err := newSum(cfg, summarize.CloudAuth{})
	if err != nil {
		fmt.Fprintf(stderr, "catchm eval: %v\n", err)
		return 1
	}

	results := eval.Run(ctx, sum, golds, func(name string) {
		fmt.Fprintf(stderr, "evaluating %s…\n", name)
	})
	out := eval.NewOutput(p, modelOf(cfg, p), results)
	if *asJSON {
		err = out.WriteJSON(stdout)
	} else {
		err = out.WriteTable(stdout)
	}
	if err != nil {
		fmt.Fprintf(stderr, "catchm eval: %v\n", err)
		return 1
	}
	if out.Evaluated == 0 {
		fmt.Fprintln(stderr, "catchm eval: no file could be evaluated")
		return 1
	}
	return 0
}

func modelOf(cfg *config.Config, provider string) string {
	if provider == summarize.ProviderOpenAI {
		_, _, strong := cfg.LLM.Resolved()
		return strong
	}
	return cfg.Summary.OllamaModel
}

func evalMain(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runEval(ctx, args, os.Stdout, os.Stderr, summarize.New)
}
