#!/bin/bash

# A script to copy a file in a Git repository while explicitly preserving its history.
# This uses a 'git mv' followed by a 'git restore' to create the strongest possible
# link for Git's rename/copy detection, which is especially useful for the GitHub UI blame view.

# --- USAGE ---
# ./git_copy_with_history.sh <source_file> <destination_file>
#
# Example:
# ./git_copy_with_history.sh cmd/folder1/file1 cmd/folder2/file2
# -------------

# Check if the correct number of arguments are provided
if [ "$#" -ne 2 ]; then
    echo "Usage: $0 <source_file> <destination_file>"
    exit 1
fi

SOURCE_FILE=$1
DEST_FILE=$2

# Check if the source file exists and is tracked by Git
if [ ! -f "$SOURCE_FILE" ] || ! git ls-files --error-unmatch "$SOURCE_FILE" > /dev/null 2>&1; then
    echo "Error: Source file '$SOURCE_FILE' does not exist or is not tracked by Git."
    exit 1
fi

# Check if the destination file already exists
if [ -f "$DEST_FILE" ]; then
    echo "Error: Destination file '$DEST_FILE' already exists."
    exit 1
fi

echo "--- Step 1: Renaming '$SOURCE_FILE' to '$DEST_FILE' ---"
# This is the key step. 'git mv' explicitly tells Git that the file at
# the new path is the successor of the file at the old path.
git mv "$SOURCE_FILE" "$DEST_FILE"

echo "--- Step 2: Restoring the original file from the previous commit ---"
# Now that we've "moved" the file, we restore the original file back
# to its place from the state it was in before this operation (HEAD).
git restore --source=HEAD "$SOURCE_FILE"

echo "--- Verification: Checking git status ---"
# Git will now see one "renamed" file (which is what we want for history)
# and one "new file" (the restored original). In some git versions,
# it might intelligently show this as a copy.
git status

echo ""
echo "✅ Success! The file has been copied."
echo "You now have:"
echo "  - '$DEST_FILE' (a copy with linked history)"
echo "  - '$SOURCE_FILE' (restored to its original state)"
echo ""
echo "Please review the changes with 'git status' and then commit the results."