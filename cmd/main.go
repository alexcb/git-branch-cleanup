package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/hyperjumptech/beda"
	"github.com/jessevdk/go-flags"
)

func die(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, msg, args...)
	os.Exit(1)
}

func runCommandTrimmedOutput(args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("no command given")
	}
	cmd := exec.Command(args[0], args[1:]...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
func runCommandSplitLines(args ...string) ([]string, error) {
	out, err := runCommandTrimmedOutput(args...)
	if err != nil {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

const branchPrefix = "refs/heads/"

func getBranches() ([]string, error) {
	lines, err := runCommandSplitLines("git", "for-each-ref", "--format=%(refname)", branchPrefix)
	if err != nil {
		return nil, err
	}
	branches := []string{}
	for _, line := range lines {
		branch := strings.TrimPrefix(strings.TrimSpace(line), branchPrefix)
		if branch != "" {
			branches = append(branches, branch)
		}
	}
	return branches, nil
}

func getGitRevParse(s string) (string, error) {
	return runCommandTrimmedOutput("git", "rev-parse", s)
}

func getGitMergeBase(a, b string) (string, error) {
	return runCommandTrimmedOutput("git", "merge-base", a, b)
}

func getCommitSubject(commit string) (string, error) {
	return runCommandTrimmedOutput("git", "--no-pager", "show", "--format=format:%s", "-s", commit)
}

func getCommitDiffOnly(commit string) (string, error) {
	contents, err := runCommandTrimmedOutput("git", "--no-pager", "show", commit)
	if err != nil {
		return "", err
	}
	parts := strings.SplitN(contents, "\ndiff --git", 2)
	if len(parts) != 2 {
		return "", nil // empty
	}
	return "diff --git" + parts[1], nil
}

type CommitDiff struct {
	Sha     string
	Subject string
	Diff    string
}

var CommitDiffCache map[string]*CommitDiff

func getCommitDiff(commit string) (*CommitDiff, error) {
	if CommitDiffCache == nil {
		CommitDiffCache = map[string]*CommitDiff{}
	}
	if commitDiff, ok := CommitDiffCache[commit]; ok {
		return commitDiff, nil
	}
	var commitDiff CommitDiff
	var err error
	commitDiff.Sha = commit
	commitDiff.Diff, err = getCommitDiffOnly(commit)
	if err != nil {
		return nil, err
	}
	commitDiff.Subject, err = getCommitSubject(commit)
	if err != nil {
		return nil, err
	}
	CommitDiffCache[commit] = &commitDiff
	return &commitDiff, nil
}

func getCommits(start, end string) ([]string, error) {
	lines, err := runCommandSplitLines("git", "log", "--format=format:%H", start+".."+end)
	if err != nil {
		return nil, err
	}
	commits := []string{}
	for _, line := range lines {
		commit := strings.TrimSpace(line)
		if commit != "" {
			commits = append(commits, commit)
		}
	}
	return commits, nil
}

func isMerged(currentBranch, branch string, perfectOnly bool) error {
	var highestSubjectScore float32
	var highestDiff *CommitDiff

	base, err := getGitMergeBase(currentBranch, branch)
	if err != nil {
		return err
	}

	branchSha, err := getGitRevParse(branch)
	if err != nil {
		return err
	}

	if base == branchSha {
		fmt.Printf("git branch -D %s\n", branch)
		return nil
	}

	branchCommits, err := getCommits(base, branch)
	if err != nil {
		return err
	}
	if len(branchCommits) != 1 {
		fmt.Fprintf(os.Stderr, "WARNING: %s contains %d commits, skipping\n\n", branch, len(branchCommits))
		return nil
	}
	//TODO squash branches that contain more than one commit
	branchDiff, err := getCommitDiff(branch)
	if err != nil {
		return err
	}

	commits, err := getCommits(base, currentBranch)
	if err != nil {
		return err
	}
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

		sd := beda.NewStringDiff(branchDiff.Subject, commitDiff.Subject)
		subjectScore := sd.JaroWinklerDistance(0.1)

		if subjectScore > highestSubjectScore {
			highestSubjectScore = subjectScore
			highestDiff = commitDiff
		}
	}

	if highestSubjectScore > 0.9 {
		if perfectOnly {
			if len(branchDiff.Diff) > 100 && branchDiff.Diff == highestDiff.Diff {
				fmt.Printf("git branch -D %s\n", branch)
			}
			return nil
		}

		// check that the diff contents match too
		sd := beda.NewStringDiff(branchDiff.Diff, highestDiff.Diff)
		diffScore := sd.JaroWinklerDistance(0.1)
		if diffScore > 0.9 {
			if len(branchDiff.Diff) > 100 && diffScore == 1.0 {
				fmt.Printf("perfect diff contents match (subject score %f)\n", highestSubjectScore)
			} else {
				fmt.Printf("subject score: %f; diff score: %f\n", highestSubjectScore, diffScore)
			}
			fmt.Printf("meld <(git show %s) <(git show %s)\n", branch, highestDiff.Sha)
			fmt.Printf("git branch -D %s\n", branch)
			fmt.Printf("\n")
		}
	}

	return nil
}

type opts struct {
	Verbose bool `long:"verbose" short:"v" description:"Enable verbose logging"`
	Version bool `long:"version" short:"V" description:"Print version and exit"`
	Perfect bool `long:"perfect" description:"only display perfect matches"`
}

func main() {
	progName := "git-branch-cleanup"
	if len(os.Args) > 0 {
		progName = os.Args[0]
	}

	progOpts := opts{}
	p := flags.NewNamedParser("", flags.PrintErrors|flags.PassDoubleDash|flags.PassAfterNonOption)
	_, err := p.AddGroup(fmt.Sprintf("%s [options] args", progName), "", &progOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}
	_, err = p.ParseArgs(os.Args[1:])
	if err != nil {
		p.WriteHelp(os.Stderr)
		os.Exit(1)
	}

	branches, err := getBranches()
	if err != nil {
		die("failed to get branches: %v\n", err)
	}

	currentBranch := "main" // TODO use the actual current branch
	for _, branch := range branches {
		if branch == currentBranch {
			continue // dont try to delete the current branch (e.g. main)
		}
		err := isMerged(currentBranch, branch, progOpts.Perfect)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ignoring %s due to: %s\n", branch, err)
		}
	}
}
