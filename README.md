# 🚀 Git Helper

A comprehensive, user-friendly terminal UI for managing your daily git workflow with smart features and intuitive controls.

## ✨ Features

### Core Git Operations
- **Individual File Staging**: Stage/unstage files individually with space bar
- **Smart Commit Suggestions**: AI-powered analysis of git diffs to suggest contextual commit messages
- **Branch Management**: Create, switch, delete branches with ease
- **Commit History**: View recent commits with detailed information
- **Diff Viewer**: View file changes before committing
- **Pull & Push**: Seamless remote synchronization

### Advanced Features
- **Conventional Commits**: Automatic formatting using conventional commit standards (type(scope): description)
- **Function Detection**: Recognizes when you add/modify functions and suggests specific messages
- **Commit Hook Management**: Install/remove git hooks to enforce commit message validation
- **Enhanced Status Display**: Clear indicators showing Staged/Unstaged/Both status for files
- **Confirmation Dialogs**: Safety checks for destructive operations
- **Real-time Updates**: Live git status with ahead/behind indicators

## 🎯 How to Use

### Six Powerful Tabs:

1. **📁 Files Mode** (`1` key)
   - View all changed files with detailed status (Staged/Unstaged/Both)
   - `Space` - Toggle stage/unstage individual files
   - `v` - View diff of selected file
   - `a` - Stage all files
   - `R` - Reset all staged files
   - See file types and suggested scopes

2. **💡 Suggestions Mode** (`2` key)
   - **Combined suggestion** as the first option - intelligently merges all your changes
   - Individual file-specific suggestions based on actual code changes
   - Analyzes git diffs, detects function additions/modifications
   - Follows conventional commit standards (feat, fix, docs, etc.)
   - `Enter` - Commit with selected suggestion
   - `e` - Edit suggestion before committing

3. **✏️ Custom Mode** (`3` key)
   - Write your own commit message
   - Full control with conventional commit validation
   - `Enter` - Commit with custom message
   - `Esc` - Cancel and return

4. **🌿 Branches Mode** (`4` key)
   - View all branches with current branch indicator
   - See upstream tracking information
   - `Enter` - Switch to selected branch
   - `n` - Create new branch
   - `d` - Delete selected branch (with confirmation)

5. **📜 History Mode** (`5` key)
   - View last 20 commits
   - See commit hash, message, author, and date
   - Quick reference for recent work

6. **📤 Output Mode** (`6` key)
   - View detailed push output
   - See last commit information
   - Track what was pushed to remote

### Quick Actions (Available in most modes):
- `p` - Git push to remote
- `l` - Git pull from remote
- `s` - Git status check
- `r` - Refresh/reload changes
- `A` - Amend last commit
- `h` - Install commit validation hook
- `H` - Remove commit validation hook
- `i` - Check hook status
- `?` - Show commit format help
- `q` - Quit

### Navigation:
- `1-6` - Switch between tabs
- `↑`/`↓` or `j`/`k` - Navigate lists
- `Enter` - Select/confirm action
- `Esc` - Cancel or go back
- `Space` - Toggle file staging (in Files mode)

## 🎨 Visual Design

The interface maintains a clean, modern look with:
- Intuitive icons and colors
- Clear status messages
- Helpful keyboard shortcuts
- Real-time feedback

## 🚦 Common Workflows

### Quick Commit & Push
1. Make your code changes
2. Run `git-helper`
3. Press `a` to stage all files (or `Space` on individual files in tab 1)
4. Press `2` to see suggestions, select one with `Enter`
5. Press `p` to push
6. Done! 🎉

### Branch & Feature Work
1. Run `git-helper`
2. Press `4` to go to Branches
3. Press `n` to create a new feature branch
4. Make your changes
5. Stage, commit, and push as above
6. Switch back to main with `4` → `Enter`

### Review Before Commit
1. Run `git-helper`
2. Press `Space` on files to stage them individually
3. Press `v` on files to view their diffs
4. Verify changes look correct
5. Press `2` for suggestions or `3` for custom message
6. Commit and push

### Pull Latest Changes
1. Run `git-helper`
2. Press `l` to pull from remote
3. Review any new changes
4. Continue your work

## 📦 Installation

### Global Installation

1. Build the application:
   ```bash
   go build -o git-helper .
   ```

2. Install to your local bin directory:
   ```bash
   cp git-helper ~/.local/bin/
   ```

3. Make sure `~/.local/bin` is in your PATH (add to your shell config if needed):
   ```bash
   export PATH="$HOME/.local/bin:$PATH"
   ```

4. Now you can use `git-helper` from any git repository!

### Usage
Simply run from any git repository:
```bash
git-helper
```