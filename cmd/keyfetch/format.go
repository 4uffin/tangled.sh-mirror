package main

import (
	"fmt"
)

func formatKeyData(repoguardPath, gitDir, logPath, endpoint string, data []map[string]interface{}) string {
	var result string
	for _, entry := range data {
		result += fmt.Sprintf(
			`command="%s -base-dir %s -user %s -log-path %s -internal-api %s",no-port-forwarding,no-X11-forwarding,no-agent-forwarding,no-pty %s`+"\n",
			repoguardPath, gitDir, entry["did"], logPath, endpoint, entry["key"])
	}
	return result
}
