package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/hyperjumptech/beda"
)

func die(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, msg, args...)
	os.Exit(1)
}

const branchPrefix = "refs/heads/"

func getBranches() ([]string, error) {
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname)", branchPrefix)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	branches := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		branch := strings.TrimPrefix(strings.TrimSpace(line), branchPrefix)
		if branch != "" {
			branches = append(branches, branch)
		}
	}
	return branches, nil
}

func getGitMergeBase(a, b string) (string, error) {
	cmd := exec.Command("git", "merge-base", a, b)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(out))
	return sha, nil
}

func getRemoteBranches(sha string) ([]string, error) {
	cmd := exec.Command("git", "branch", "-r", "--contains", sha)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	branches := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		branch := strings.TrimSpace(line)
		if branch != "" {
			branches = append(branches, branch)
		}
	}
	return branches, nil
}

func getRemoteURL(remoteName string) (string, error) {
	cmd := exec.Command("git", "config", "--get", "remote."+remoteName+".url")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

const sshGitPrefix = "git@github.com:"

func formatGithubURL(user, repo, gitSha, path string, line int) string {
	url := fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", user, repo, gitSha, path)
	if line >= 0 {
		url += fmt.Sprintf("#L%d", line)
	}
	return url
}

func getUserAndRepo(s string) (string, string, error) {
	parts := strings.Split(strings.TrimSuffix(s, ".git"), "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("failed to split %s", s)
	}
	return parts[0], parts[1], nil
}

func getGithubURL(gitURL, gitSha, path string, line int) (string, error) {
	var urlPath string
	if strings.HasPrefix(gitURL, sshGitPrefix) {
		urlPath = strings.TrimPrefix(gitURL, sshGitPrefix)
	} else {
		u, err := url.Parse(gitURL)
		if err != nil {
			return "", err
		}
		urlPath = u.Path
	}
	user, repo, err := getUserAndRepo(urlPath)
	if err != nil {
		return "", err
	}
	return formatGithubURL(user, repo, gitSha, path, line), nil
}

func isFileTracked(path string) error {
	cmd := exec.Command("git", "ls-files", "--error-unmatch", path)
	_, err := cmd.Output()
	if err != nil {
		return err
	}
	return nil

}

func getCommitDiff(commit string) (string, error) {
	cmd := exec.Command("git", "--no-pager", "show", commit)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func getCommits(start, end string) ([]string, error) {
	cmd := exec.Command("git", "log", "--format=format:%H", start+".."+end)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	commits := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		commit := strings.TrimSpace(line)
		if commit != "" {
			commits = append(commits, commit)
		}
	}
	return commits, nil
}

func isMerged(currentBranch, branch string) error {
	//TODO squash branches that contain more than one commit
	branchDiff, err := getCommitDiff(branch)
	if err != nil {
		return err
	}

	var highest float32
	var highestSha string

	base, err := getGitMergeBase(currentBranch, branch)
	if err != nil {
		return err
	}
	commits, err := getCommits(base, currentBranch)
	for _, commit := range commits {
		commitDiff, err := getCommitDiff(commit)
		if err != nil {
			return err
		}

		// for a good match
		// Levenshtein Distance is 70
		// Trigram Compare is is 70
		// Jaro Distance is is 0.84454864
		// Jaro Wingkler Distance is 0.92227435

		sd := beda.NewStringDiff(branchDiff, commitDiff)
		jwDiff := sd.JaroWinklerDistance(0.1)
		//fmt.Printf("%s %v\n", commit, jwDiff)

		if jwDiff > highest {
			highest = jwDiff
			highestSha = commit
		}
	}

	if highest > 0.9 {
		fmt.Printf("meld <(git show %s) <(git show %s)\n", branch, highestSha)
		fmt.Printf("git branch -D %s\n", branch)
		fmt.Printf("\n")
	}

	return nil
}

func main() {
	progName := "git-branch-cleanup"
	if len(os.Args) > 0 {
		progName = os.Args[0]
	}

	if len(os.Args) != 1 {
		die("usage: %s\n", progName)
	}

	branches, err := getBranches()
	if err != nil {
		die("failed to get branches: %v\n", err)
	}

	for _, branch := range branches {
		//if branch != "acb/network=none" {
		//	continue
		//}
		_ = isMerged("main", branch)
	}
}
