// Package messages holds every sentence the tool prints; flipping the language means editing this file only.
// A sentence that starts with "Error:" goes to stderr.
package messages

import "github.com/shoootyou/litegauge/internal/verdict"

// Name is the program name: the Use of the root, the User-Agent product and the start of every example line.
const Name = "litegauge"

// Command is the text of one command. Example holds one command per line, each starting with the program name.
type Command struct{ Use, Short, Long, Example string }

var Root = Command{
	Use:   Name + " COMMAND",
	Short: "Checks whether the keys of a LiteLLM gateway have budget.",
	Long: `Checks whether the keys of a LiteLLM gateway have budget for a model.
Each command sends minimal requests and gives a verdict per key and model.
The URL goes in LITEGAUGE_URL; the key goes in LITEGAUGE_KEY or in --keys-file.`,
	Example: `litegauge models
litegauge probe claude-sonnet-5-5 -o json
litegauge matrix --keys-file keys.txt`,
}

var Models = Command{
	Use:   "models",
	Short: "Lists the text models the gateway serves to the key.",
	Long: `Asks the gateway for its model list and shows the text models with their price.
It sends no chat request.
With several keys in --keys-file, pick one with --name.`,
	Example: `litegauge models
litegauge models -o yaml
litegauge models --keys-file keys.txt --name Alpha`,
}

var Probe = Command{
	Use:   "probe MODEL",
	Short: "Tests one key against one model with a single request.",
	Long: `Sends a minimal request to /v1/chat/completions with one key and gives its verdict.
Exits 0 if the result is ok or ok*, and 1 if it is not.
With several keys in --keys-file, pick one with --name.`,
	Example: `litegauge probe claude-sonnet-5-5
litegauge probe claude-sonnet-5-5 -o json
litegauge probe claude-opus-5-5 --keys-file keys.txt --name Alpha`,
}

var Matrix = Command{
	Use:   "matrix",
	Short: "Tests every model with every key.",
	Long: `Sends one minimal request per model and key, and gives a verdict per cell.
Without --models, it tests every text model the gateway serves.
Exits 0 if every cell is ok or ok*, and 1 if any cell is not.`,
	Example: `litegauge matrix --keys-file keys.txt
litegauge matrix --keys-file keys.txt -o json
litegauge matrix --keys-file keys.txt --models claude-sonnet-5-5,claude-opus-5-5`,
}

var Flags = struct{ Output, KeysFile, Name, Models, Help, Version string }{
	Output:   "format: table, json or yaml",
	KeysFile: "keys file (one name=key per line), or - for stdin",
	Name:     "name of the key to use from the file",
	Models:   "comma-separated models (default: every text model)",
	Help:     "show this help",
	Version:  "show the version",
}

var Help = struct{ Header, Usage, Commands, Flags, Examples, Footer string }{
	Header:   "Litegauge %s",
	Usage:    "Usage:",
	Commands: "Commands:",
	Flags:    "Flags:",
	Examples: "Examples:",
	Footer:   `Use "litegauge COMMAND -h" for help on a command.`,
}

var Err = struct {
	NoModel, BadFormat, NoURL, BadURL, NoKey, BadEnvKey, KeyArg, BadLine, KeysFile, ManyKeys, NoNamedKey, BadArg, ListFailed, Internal string
	NoReply, NoModels, HTTPStatus                                                                                                      string
}{
	NoModel:    "Error: missing model.",
	BadFormat:  "Error: unknown format; use table, json or yaml.",
	NoURL:      "Error: missing LITEGAUGE_URL.",
	BadURL:     "Error: LITEGAUGE_URL must be https (or http on localhost).",
	NoKey:      "Error: missing key: use LITEGAUGE_KEY or --keys-file.",
	BadEnvKey:  "Error: the key in LITEGAUGE_KEY is not valid.",
	KeyArg:     "Error: the key does not go on the command line; use LITEGAUGE_KEY or --keys-file.",
	BadLine:    "Error: line %d of the keys file is not valid.",
	KeysFile:   "Error: could not read the keys file.",
	ManyKeys:   "Error: the file has %d keys; pick one with --name.",
	NoNamedKey: "Error: no key named %q.",
	BadArg:     "Error: invalid argument.",
	ListFailed: "Error: could not list the models (%s).",
	Internal:   "Error: internal error.",
	// Reasons that fill ListFailed.
	NoReply:    "no response",
	NoModels:   "no models",
	HTTPStatus: "HTTP %d",
}

// Labels are the screen texts: table headers, the legend, the latency line and the spinner.
var Labels = struct{ Key, Model, Result, Detail, Price, Legend, HTTPLatency, LatencyOnly, Spinner string }{
	Key:         "Key",
	Model:       "Model",
	Result:      "Result",
	Detail:      "Detail",
	Price:       "Price",
	Legend:      "ok* = valid key, free model (budget not checked)",
	HTTPLatency: "HTTP %d · %d ms",
	LatencyOnly: "%d ms",
	Spinner:     "Probing %s with %s...",
}

// VerdictText: Word is the capitalised verdict of the probe screen, Cell the short form inside a matrix.
type VerdictText struct{ Symbol, Word, Cell, Detail string }

var Verdicts = map[verdict.Verdict]VerdictText{
	verdict.OK:            {"✓", "Ok", "ok", "The key has budget for this model."},
	verdict.KeyValid:      {"✓", "Valid key", "ok*", "Valid key; free or unpriced model, budget not checked."},
	verdict.NoBudget:      {"✗", "No budget", "budget", "The budget of this key for this model is used up."},
	verdict.Blocked:       {"⊘", "Blocked by policy", "policy", "The gateway policy blocks this model."},
	verdict.InvalidKey:    {"✗", "Invalid key", "key", "The key is not valid or has expired."},
	verdict.ModelNotFound: {"?", "Model not found", "model", "The gateway does not know this model."},
	verdict.Error:         {"?", "Error", "error", ""},
}

// ErrorDetail is the Detail of an error verdict, chosen by the HTTP status (0 means no response arrived).
var ErrorDetail = struct{ NoReply, Redirect, Rate, Server, Other string }{
	NoReply:  "No response from the gateway.",
	Redirect: "Redirect not followed.",
	Rate:     "Gateway rate limit.",
	Server:   "Server error.",
	Other:    "Unexpected response.",
}

type PriceText struct{ Long, Short string }

var Prices = map[verdict.Price]PriceText{
	verdict.Free:    {"free", "free"},
	verdict.Paid:    {"paid", "paid"},
	verdict.Unknown: {"unknown", "unknown"},
}
