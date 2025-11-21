# 🚀 Git Helper

A comprehensive, beautifully designed terminal UI for mastering your daily git workflow with smart commit suggestions and powerful advanced features.

## ✨ What's New in v2.0

**Complete UI Redesign** - Clean 4-tab architecture for better workflow
- **Enhanced Commit Intelligence** - Semantic analysis with keyword detection, variable extraction, and context awareness
- **Merge Conflict Resolution** - Visual conflict viewer with quick resolution options
- **Branch Comparison** - See exactly what differs between branches
- **Undo/Revert Tools** - Safe ways to undo commits (soft, mixed, hard reset, reflog)
- **Interactive Rebase** - Squash, reorder, reword, drop commits visually

## 🎯 The Four Tabs

### Tab 1: 📁 WORKSPACE
Your file staging hub with live diff previews

**Features:**
- Individual file staging with space bar
- Live diff preview panel (toggle with `v`)
- Auto-detecting merge conflicts
- Visual conflict resolution when detected
- Color-coded file status indicators

**Shortcuts:**
- `Space` - Stage/unstage selected file
- `a` - Stage all files
- `R` - Reset/unstage all files
- `v` - Toggle diff preview panel
- `d` - View full diff of selected file
- `r` - Refresh changes

**Conflict Mode** (auto-activates when conflicts detected):
- `o` - Accept ours
- `t` - Accept theirs
- `b` - Accept both
- `c` - Continue merge (when all resolved)

---

### Tab 2: 💡 COMMIT
Unified commit interface combining smart suggestions with custom input

**Features:**
- Up to 9 numbered smart suggestions based on semantic analysis
- Custom commit message input (always visible)
- Last 3 commits shown for reference
- Conventional commit format validation
- Only accessible when files are staged

**How It Works:**
The app analyzes your changes and detects:
- Keywords: bug, fix, error, validate, security, optimize, cache
- Function names in Go, JS, TS, Python, Java, C#
- Variable names and imports
- Code comments for context
- Change patterns (additions, refactors, fixes)

**Shortcuts:**
- `1-9` - Instantly commit with that numbered suggestion
- `Enter` or `c` - Type custom message
- `↑`/`↓` - Navigate suggestions
- `Space` - Commit with selected suggestion

**Example Suggestions:**
```
[1] feat(auth): add validation to userInput
[2] fix(auth): fix error handling in processPayment
[3] refactor(api): optimize database query performance
```

---

### Tab 3: 🌿 BRANCHES
Complete branch management with comparison tools

**Features:**
- View all branches (local and remote) with current indicator
- See ahead/behind counts for each branch
- Compare current branch with main/master
- Create, switch, delete branches
- Switch to remote branches (creates local tracking branch)
- Remote branches marked with 📡 icon
- Safe delete with confirmations

**Shortcuts:**
- `Enter` - Switch to selected branch (local or remote)
- `n` - Create new branch
- `d` - Delete branch (with confirmation)
- `c` - Compare with main/master
- `f` - Fetch from remote (sync remote branches)
- `r` - Refresh branches
- `y` - Confirm deletion

**Branch Comparison:**
Shows three sections when comparing:
1. **Commits Ahead** - Commits on your branch not in target
2. **Commits Behind** - Commits in target not on your branch
3. **Differing Files** - All files changed between branches

---

### Tab 4: 🛠️ TOOLS
Advanced operations menu (press 1-4 to select)

#### 1. Undo/Revert
Safe ways to undo commits:
- `1` - Soft reset (undo commit, keep changes staged)
- `2` - Mixed reset (undo commit, unstage changes)
- `3` - Hard reset (undo commit, DISCARD changes) ⚠️
- `4` - View reflog (recover lost work)
- `y` - Confirm action after selecting

#### 2. Interactive Rebase
Rewrite commit history visually:
- Enter number of commits to rebase
- Navigate commits with ↑/↓
- Press action keys to change each commit:
  - `p` - Pick (use commit as-is)
  - `s` - Squash (combine with previous)
  - `r` - Reword (change message)
  - `d` - Drop (remove commit)
  - `f` - Fixup (squash, discard message)
- `Enter` - Execute rebase plan
- `y` - Confirm execution

#### 3. History & Reflog
Enhanced commit history:
- View last 20 commits
- Full hash, message, author, date
- `r` - Refresh history
- `c` - Copy hash to clipboard

#### 4. Remote Operations
Push/pull with detailed output:
- `p` - Git push
- `l` - Git pull
- `f` - Git fetch
- See detailed results and last commit info

---

## 🚦 Common Workflows

### Quick Commit & Push
```
1. Make code changes
2. Run git-helper
3. Press 'a' to stage all (or Space on individual files)
4. Press '2' to see smart suggestions
5. Press '1' to commit with first suggestion (or type custom)
6. Tools > Remote > 'p' to push
```

### Branch Feature Work
```
1. Tab 3 > 'n' > create feature branch
2. Make changes
3. Tab 1 > stage files
4. Tab 2 > commit with smart suggestion
5. Tab 4 > Remote > 'p' to push
6. Tab 3 > Switch back to main
```

### Review & Compare
```
1. Tab 3 > 'c' to compare with main
2. Review ahead/behind commits
3. Check differing files
4. Tab 1 > review your changes with diff preview
5. Commit when ready
```

### Fix Mistake with Undo
```
1. Oh no, wrong commit!
2. Tab 4 > Undo/Revert
3. Press '1' for soft reset (keeps changes)
4. Press 'y' to confirm
5. Fix and recommit
```

### Clean History with Rebase
```
1. Tab 4 > Interactive Rebase
2. Enter "5" for last 5 commits
3. Press 's' on commits to squash
4. Press 'r' on commits to reword
5. Enter to execute, 'y' to confirm
6. Clean history!
```

### Resolve Merge Conflicts
```
1. Merge causes conflicts
2. git-helper auto-detects, shows Conflicts view
3. Navigate files with ↑/↓
4. Press 'o' (ours), 't' (theirs), or 'b' (both)
5. Repeat for all conflicts
6. Press 'c' to continue merge
```

---

## 🎨 Visual Design

**Clean Modern Interface:**
- Color-coded status bar (branch, staged/unstaged, ahead/behind)
- Syntax-highlighted diffs (green additions, red deletions)
- Context-sensitive help in footer
- Intuitive tab navigation (1-4 keys)
- Visual feedback for all actions

**Smart Status Indicators:**
- ✅ Staged
- 📝 Modified
- ➕ Added
- ➖ Deleted
- ⚡ Both (staged + modified)
- ⚠️ Conflicts

---

## 🤖 Smart Commit Suggestions

The intelligence engine analyzes your diffs and generates contextual commit messages:

**What It Detects:**
- **Keywords**: bug, fix, error, validate, auth, security, optimize, cache, api, endpoint
- **Functions**: Recognizes function additions/changes in multiple languages
- **Variables**: Extracts meaningful variable names
- **Comments**: Uses descriptive comments as commit messages
- **Context**: Classifies changes as fix, validation, API, security, performance

**Examples:**

```diff
+ function validateUserInput(data) {
+   if (!data.email) throw new Error("Invalid email");
+ }
```
→ `feat(validation): add validation to userInput`

```diff
- if (user) {
+ if (user && user.isActive) {
+   // Fix: Check user active status before auth
```
→ `fix(auth): fix error handling in authentication`

```diff
+ const cache = new Map();
+ function getCached(key) {
```
→ `feat(performance): optimize with caching`

---

## 📦 Installation

### Build & Install Globally

```bash
# Clone or download
cd git-helper

# Build
go build -o git-helper .

# Install to local bin
cp git-helper ~/.local/bin/

# Add to PATH (if not already)
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

### Usage

Run from any git repository:
```bash
git-helper
```

---

## 🎓 Pro Tips

1. **Use number keys in Commit tab** - Pressing 1-9 instantly commits with that suggestion
2. **Toggle diff preview** - Press 'v' in Workspace to see changes without leaving tab
3. **Compare before merge** - Tab 3 > 'c' shows exactly what will change
4. **Reflog is your safety net** - Tools > Undo > View reflog can recover "lost" commits
5. **Squash WIP commits** - Use interactive rebase to clean up before pushing
6. **Space bar is your friend** - Stage individual files for atomic commits
7. **Branch comparison** - See what's different before pulling

---

## 🔧 Configuration

### Git Hooks
Press `h` in any tab to install a commit message validation hook that enforces conventional commit format.

Press `H` to remove the hook.

Press `i` to check if the hook is installed.

---

## 📝 Conventional Commits

This tool follows the [Conventional Commits](https://www.conventionalcommits.org/) standard:

**Format:** `type(scope): description`

**Types:**
- `feat` - New feature
- `fix` - Bug fix
- `docs` - Documentation only
- `style` - Code style (formatting, no logic change)
- `refactor` - Code refactoring
- `test` - Adding tests
- `chore` - Maintenance (deps, config, etc.)

**Examples:**
- `feat(auth): add OAuth2 integration`
- `fix(api): fix null pointer in user endpoint`
- `docs(readme): update installation instructions`
- `refactor(db): optimize query performance`

---

## 🤝 Contributing

Built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI framework.

Feedback and contributions welcome!

---

## 📜 License

MIT License - feel free to use and modify!

---

**Made for developers who want a better git workflow. Happy committing! 🎉**
