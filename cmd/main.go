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

func getCurrentBranch() (string, error) {
	s, err := runCommandTrimmedOutput("git", "symbolic-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(s, "refs/heads/"), nil
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

// NOTE: this does not return the start commit, but DOES include the end commit
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

// git --no-pager show HEAD is equivalent to git --no-pager diff HEAD^..HEAD **except** show will also show the commit time/author/subject/message details
// Note that this combines the diffs of commits from start to end INCLUSIVE
func getGitDiff(start, end string) (string, error) {
	return runCommandTrimmedOutput("git", "--no-pager", "diff", start+".."+end)
}

type PotentialMerge struct {
	Branch       string
	MergedSha    string
	Merged       bool // true when the branch sha matches the merged sha (i.e. no rewritten history)
	SubjectScore float32
	DiffScore    float32
	DiffSize     int
	NumCommits   int
	DiffCmd      string
}

func findMerged(currentBranch, branch string) (*PotentialMerge, error) {
	var highestSubjectScore float32
	var highestDiff *CommitDiff

	base, err := getGitMergeBase(currentBranch, branch)
	if err != nil {
		return nil, err
	}

	branchSha, err := getGitRevParse(branch)
	if err != nil {
		return nil, err
	}

	if base == branchSha {
		return &PotentialMerge{
			Branch:       branch,
			MergedSha:    base,
			Merged:       true,
			SubjectScore: 1.00,
			DiffScore:    1.00,
			NumCommits:   0,
		}, nil
	}

	branchCommits, err := getCommits(base, branch)
	if err != nil {
		return nil, err
	}
	if len(branchCommits) == 0 {
		panic("branchCommits is empty, but if base == branchSha check didnt catch this")
	}

	var combinedDiff string
	var highestCombinedDiff string
	var branchDiff *CommitDiff
	branchDiff, err = getCommitDiff(branchCommits[0])
	if err != nil {
		return nil, err
	}
	if len(branchCommits) > 1 {
		combinedDiff, err = getGitDiff(base, branch)
		if err != nil {
			return nil, err
		}
	}

	commits, err := getCommits(base, currentBranch)
	if err != nil {
		return nil, err
	}
	for _, commit := range commits {
		commitDiff, err := getCommitDiff(commit)
		if err != nil {
			return nil, err
		}

		sd := beda.NewStringDiff(branchDiff.Subject, commitDiff.Subject)
		subjectScore := sd.JaroWinklerDistance(0.1)

		if subjectScore > highestSubjectScore {
			highestSubjectScore = subjectScore
			highestDiff = commitDiff
			highestCombinedDiff = combinedDiff
		}
	}

	if highestDiff == nil {
		return nil, nil
	}

	var diffScore float32
	if highestCombinedDiff == "" {
		// check that the diff contents match too
		sd := beda.NewStringDiff(branchDiff.Diff, highestDiff.Diff)
		diffScore = sd.JaroWinklerDistance(0.1)

		if 1 != len(branchCommits) {
			panic("expected single commit")
		}

		return &PotentialMerge{
			Branch:       branch,
			MergedSha:    branchDiff.Sha,
			SubjectScore: highestSubjectScore,
			DiffScore:    diffScore,
			DiffSize:     len(branchDiff.Diff),
			NumCommits:   1,
			DiffCmd:      fmt.Sprintf("meld <(git show %s) <(git show %s)", branch, highestDiff.Sha),
		}, nil
	}

	// otherwise we are dealing with a branch that has been squashed

	combinedDiff, err = getGitDiff(highestDiff.Sha+"^", highestDiff.Sha)
	if err != nil {
		return nil, err
	}

	sd := beda.NewStringDiff(combinedDiff, highestCombinedDiff)
	diffScore = sd.JaroWinklerDistance(0.1)

	return &PotentialMerge{
		Branch:       branch,
		MergedSha:    branchDiff.Sha,
		SubjectScore: highestSubjectScore,
		DiffScore:    diffScore,
		DiffSize:     len(combinedDiff),
		NumCommits:   len(branchCommits),
		DiffCmd:      fmt.Sprintf("meld <(git --no-pager diff %s..%s) <(git --no-pager diff %s..%s)", base, branch, highestDiff.Sha+"^", highestDiff.Sha),
	}, nil
}

type opts struct {
	Verbose         bool    `long:"verbose" short:"v" description:"Enable verbose logging"`
	Version         bool    `long:"version" short:"V" description:"Print version and exit"`
	Perfect         bool    `long:"perfect" description:"only display perfect matches"`
	MinSubjectScore float32 `long:"min-subject-score" default:"0.9" description:"minimum subject score"`
	MinDiffScore    float32 `long:"min-diff-score"  default:"0.9" description:"minimum diff score"`
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

	currentBranch, err := getCurrentBranch()
	if err != nil {
		die("failed to get current branch: %v\n", err)
	}

	switch currentBranch {
	case "main", "master", "trunk":
		break
	default:
		die("current branch is %s; expected main, master, or trunk", currentBranch)
	}

	for _, branch := range branches {
		if branch == currentBranch {
			continue // dont try to delete the current branch (e.g. main)
		}

		potentialMerged, err := findMerged(currentBranch, branch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ignoring %s due to: %s\n", branch, err)
		}
		if potentialMerged == nil {
			continue // likely not merged
		}

		if potentialMerged.Merged {
			fmt.Printf("%s was cleanly merged under %s\n", branch, potentialMerged.MergedSha)
			fmt.Printf("git branch -D %s\n", branch)
			continue
		}

		if potentialMerged.SubjectScore > progOpts.MinSubjectScore && potentialMerged.DiffScore > progOpts.MinDiffScore {
			perfectDiffMatch := bool(potentialMerged.DiffScore == 1.0 && potentialMerged.DiffSize > 100)
			if progOpts.Perfect {
				if perfectDiffMatch {
					fmt.Printf("git branch -D %s\n", branch)
				}
				continue
			}

			if perfectDiffMatch {
				fmt.Printf("%s was merged under %s (subject score: %f; diff score %f)\n", branch, potentialMerged.MergedSha, potentialMerged.SubjectScore, potentialMerged.DiffScore)
			} else {
				fmt.Printf("%s was **potentially** merged under %s (subject score: %f; diff score %f)\n", branch, potentialMerged.MergedSha, potentialMerged.SubjectScore, potentialMerged.DiffScore)
			}
			if potentialMerged.NumCommits > 1 {
				fmt.Printf("WARNING: %s contains %d commits, comparing combined diffs instead (and ommitting commit message)\n", branch, potentialMerged.NumCommits)
			}
			fmt.Printf("%s\n", potentialMerged.DiffCmd)
			fmt.Printf("git branch -D %s\n", branch)
			fmt.Printf("\n")
		}
	}
}
