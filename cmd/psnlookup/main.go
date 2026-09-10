// Command psnlookup prints trophy counts for one PSN Online ID.
//
// NPSSO comes from PSN_NPSSO or .env. --raw prints complete Sony JSON
// (search / profile / trophySummary) and never prints tokens.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"ps-trophy-ranking/internal/psn"
)

func main() {
	onlineID, raw, help, err := parseArgs(os.Args[1:])
	if help || err != nil {
		fmt.Fprintln(os.Stderr, "usage: psnlookup [--raw] <psn-online-id>")
		fmt.Fprintln(os.Stderr, "  --raw   print full Sony API JSON responses (no tokens)")
		if err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	if !validOnlineID(onlineID) {
		fmt.Fprintln(os.Stderr, "PSN Online ID 须为 3–16 位，且以字母开头，只含字母、数字、连字符或下划线")
		os.Exit(2)
	}

	npsso := strings.TrimSpace(os.Getenv("PSN_NPSSO"))
	if npsso == "" {
		npsso = strings.TrimSpace(npssoFromDotEnv(".env"))
	}
	c := psn.New(npsso)
	if raw {
		c.EnableDump()
	}
	sum, err := c.Lookup(context.Background(), onlineID)
	if raw {
		printDumps(c.Dumps())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, userMessage(err, npsso != ""))
		os.Exit(1)
	}
	fmt.Printf("Online ID: %s\n", sum.DisplayID)
	fmt.Printf("Platinum: %d\n", sum.Platinum)
	fmt.Printf("Gold: %d\n", sum.Gold)
	fmt.Printf("Silver: %d\n", sum.Silver)
	fmt.Printf("Bronze: %d\n", sum.Bronze)
	fmt.Printf("Score: %d\n", sum.Score)
}

func parseArgs(args []string) (onlineID string, raw bool, help bool, err error) {
	var positional []string
	for _, a := range args {
		switch a {
		case "-h", "--help":
			help = true
		case "--raw", "-raw", "--json":
			raw = true
		default:
			if strings.HasPrefix(a, "-") {
				return "", false, false, fmt.Errorf("unknown flag: %s", a)
			}
			positional = append(positional, a)
		}
	}
	if help {
		return "", raw, true, nil
	}
	if len(positional) != 1 {
		return "", raw, false, fmt.Errorf("usage")
	}
	return strings.TrimSpace(positional[0]), raw, false, nil
}

func printDumps(dumps []psn.CallDump) {
	if len(dumps) == 0 {
		fmt.Fprintln(os.Stderr, "(no Sony API JSON captured)")
		return
	}
	for _, d := range dumps {
		fmt.Printf("=== %s %s (%d) ===\n", d.Method, d.URL, d.Status)
		fmt.Println(prettyJSON(d.Body))
		fmt.Println()
	}
}

func prettyJSON(body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return "(empty)"
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return string(body)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(body)
	}
	return string(out)
}

func validOnlineID(id string) bool {
	if len(id) < 3 || len(id) > 16 {
		return false
	}
	first := id[0]
	if (first < 'A' || first > 'Z') && (first < 'a' || first > 'z') {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func npssoFromDotEnv(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "PSN_NPSSO" {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		return value
	}
	return ""
}

func userMessage(err error, hasNPSSO bool) string {
	switch {
	case errors.Is(err, &psn.Error{Kind: psn.KindNoCredentials}):
		return "服务端未配置 PSN 凭证"
	case errors.Is(err, &psn.Error{Kind: psn.KindInvalidCredentials}):
		if hasNPSSO {
			return "PSN 凭证无效，请重新获取 NPSSO"
		}
		return "服务端未配置 PSN 凭证"
	case errors.Is(err, &psn.Error{Kind: psn.KindNotFound}):
		return "找不到该 PSN 用户"
	case errors.Is(err, &psn.Error{Kind: psn.KindPrivate}):
		return "该用户奖杯未公开，无法入榜"
	default:
		return "暂时无法同步奖杯，请稍后重试"
	}
}
