## git-branch-cleanup

Helps deteremine which branches to merge

    usage: git-branch-cleanup

## Building

First download earthly, then run one of the corresponding targets which matches your platform:

    earthly +git-branch-cleanup-linux-amd64
    earthly +git-branch-cleanup-linux-arm64
    earthly +git-branch-cleanup-darwin-amd64
    earthly +git-branch-cleanup-darwin-arm64

This will output a binary under `./build/...`.

## Licensing
git-branch-cleanup is licensed under the Mozilla Public License Version 2.0. See [LICENSE](LICENSE).
