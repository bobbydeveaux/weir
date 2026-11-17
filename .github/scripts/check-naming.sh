#!/usr/bin/env bash
# Checks that a pull request is named after the issue it closes:
#
#   branch   feature/issue-0-credentials        (or fix/, docs/, chore/)
#   commits  #0: Add credentials on a connection
#   title    #0: Add credentials on a connection
#   body     Closes #0
#
# Usage: check-naming.sh <branch> <title> <body-file> <base-ref>
# Prints every problem it finds and exits 1 if there is one.
set -u
branch=$1 title=$2 body_file=$3 base=$4
problems=0
fail() { echo "✗ $1"; problems=$((problems + 1)); }

if [[ $branch =~ ^(feature|fix|docs|chore)/issue-([0-9]+)-[a-z0-9]+(-[a-z0-9]+)*$ ]]; then
  issue=${BASH_REMATCH[2]}
  echo "✓ branch $branch is for issue #$issue"
else
  fail "branch \"$branch\" must look like feature/issue-0-short-name (feature, fix, docs or chore; lower case, digits and hyphens)"
  echo "Nothing else can be checked without the issue number."
  exit 1
fi

if [[ $title =~ ^#${issue}:\ [^[:space:]] ]]; then
  echo "✓ title starts with #$issue:"
else
  fail "the pull request title must start with \"#$issue: \", and is \"$title\""
fi

if grep -Eiq "(closes|fixes|resolves) #${issue}([^0-9]|$)" "$body_file"; then
  echo "✓ description closes #$issue"
else
  fail "the pull request description must say \"Closes #$issue\", so that merging closes the issue"
fi

# Every commit on the branch, except merges, names the same issue.
while IFS= read -r line; do
  [[ -z $line ]] && continue
  sha=${line%% *} subject=${line#* }
  if [[ $subject =~ ^#${issue}:\ [^[:space:]] ]]; then
    echo "✓ commit $sha: $subject"
  else
    fail "commit $sha must start with \"#$issue: \", and is \"$subject\""
  fi
done < <(git log --no-merges --format='%h %s' "$base..HEAD")

if ((problems > 0)); then
  echo
  echo "$problems problem(s). The rules are in CLAUDE.md, under \"Naming\"."
  echo "To reword a commit: git commit --amend -m \"#$issue: …\"  (or git rebase -i for an older one), then push with --force-with-lease."
  exit 1
fi
echo "All named after issue #$issue."
