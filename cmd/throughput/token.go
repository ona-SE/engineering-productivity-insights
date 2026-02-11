package main

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
)

// resolveToken tries GH_TOKEN, GITHUB_TOKEN, then git credential helper.
func resolveToken() string {
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	return tokenFromCredentialHelper()
}

func tokenFromCredentialHelper() string {
	// Get the credential helper config for github.com
	out, err := exec.Command("git", "config", "--get", "credential.github.com.helper").Output()
	if err != nil {
		return ""
	}
	helper := strings.TrimSpace(string(out))
	if helper == "" {
		return ""
	}

	// Parse the helper path: strip !f() { cat '...' ; }; f wrapper
	credFile := helper
	credFile = strings.TrimPrefix(credFile, "!f() { cat '")
	credFile = strings.TrimSuffix(credFile, "'; }; f")

	if credFile == "" {
		return ""
	}

	f, err := os.Open(credFile)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "password=") {
			return strings.TrimPrefix(line, "password=")
		}
	}
	return ""
}
