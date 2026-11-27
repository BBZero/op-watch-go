package version

import (
	"fmt"
	"strings"
)

var (
	Version = "1.0.0"
	Commit  = "unknown"
)

func String() string {
	return Version + " (" + Commit + ")"
}
func PrintVersion() {
	const (
		reset = "\033[0m"
		// cyan         = "\033[36m"
		// yellow       = "\033[33m"
		brightYellow = "\033[93m"
		// green        = "\033[32m"
		brightGreen = "\033[92m"
		innerWidth  = 13
	)
	version := "v" + Version
	paddingLen := innerWidth - len(version)
	if paddingLen < 0 {
		paddingLen = 0
	}
	versionPadding := strings.Repeat(" ", paddingLen)

	commit := "(" + Commit + ")"
	paddingLen = innerWidth - len(commit)
	if paddingLen < 0 {
		paddingLen = 0
	}
	commitPadding := strings.Repeat(" ", paddingLen)

	logo := fmt.Sprintf(`
    ╔════════════════╗
    ║  %sOpWatch%s     ║
    ║  %s%s%s%s║
    ║  %s%s║
    ╚════════════════╝
`, brightGreen, reset,
		brightYellow, version, reset, versionPadding,
		commit, commitPadding)
	fmt.Print(logo)
}
