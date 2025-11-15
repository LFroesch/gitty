package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type GitChange struct {
	File   string
	Status string
	Type   string // commit type suggestion
	Scope  string // commit scope suggestion
}

type CommitSuggestion struct {
	Message string
	Type    string
}

type GitStatus struct {
	Branch        string
	Clean         bool
	StagedFiles   int
	UnstagedFiles int
	Ahead         int
	Behind        int
}

type Branch struct {
	Name      string
	IsCurrent bool
	Upstream  string
}

type Commit struct {
	Hash    string
	Message string
	Author  string
	Date    string
}

type model struct {
	state       string // "files", "suggestions", "custom", "edit", "output", "branches", "history", "diff"
	changes     []GitChange
	suggestions []CommitSuggestion
	gitState    GitStatus
	branches    []Branch
	commits     []Commit
	diffContent string

	filesTable       table.Model
	suggestionsTable table.Model
	branchesTable    table.Model
	historyTable     table.Model
	customInput      textinput.Model
	editInput        textinput.Model
	branchInput      textinput.Model

	width        int
	height       int
	statusMsg    string
	statusExpiry time.Time

	repoPath         string
	pushOutput       string
	lastCommit       string
	lastStatusUpdate time.Time
	confirmAction    string // for confirmation dialogs
}

type statusMsg struct {
	message string
}

type gitChangesMsg []GitChange
type commitSuggestionsMsg []CommitSuggestion
type gitStatusMsg GitStatus
type branchesMsg []Branch
type commitsMsg []Commit
type diffMsg string
type pushOutputMsg struct {
	output string
	commit string
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86"))

	repositoryStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("208"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			MarginTop(1)
)

func main() {
	repoPath, err := findGitRepo()
	if err != nil {
		// Offer to initialize git repository
		fmt.Println("❌ Not in a git repository.")
		fmt.Print("Would you like to initialize a git repository here? (y/N): ")
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) == "y" || strings.ToLower(response) == "yes" {
			if err := initGitRepo(); err != nil {
				log.Fatal("Failed to initialize git repository:", err)
			}
			fmt.Println("✅ Git repository initialized successfully!")
			repoPath, _ = findGitRepo()
		} else {
			log.Fatal("Error: Not in a git repository")
		}
	}

	m := model{
		state:    "files",
		repoPath: repoPath,
		width:    100,
		height:   24,
	}

	// Initialize files table
	filesColumns := []table.Column{
		{Title: "Status", Width: 8},
		{Title: "File", Width: 50},
		{Title: "Type", Width: 12},
		{Title: "Scope", Width: 15},
	}

	filesTable := table.New(
		table.WithColumns(filesColumns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	filesStyle := table.DefaultStyles()
	filesStyle.Header = filesStyle.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	filesStyle.Selected = filesStyle.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	filesTable.SetStyles(filesStyle)

	// Initialize suggestions table
	suggestionsColumns := []table.Column{
		{Title: "Type", Width: 12},
		{Title: "Message", Width: 70},
	}

	suggestionsTable := table.New(
		table.WithColumns(suggestionsColumns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	suggestionsTable.SetStyles(filesStyle) // Use same style

	// Initialize branches table
	branchesColumns := []table.Column{
		{Title: "Current", Width: 8},
		{Title: "Branch Name", Width: 40},
		{Title: "Upstream", Width: 35},
	}
	branchesTable := table.New(
		table.WithColumns(branchesColumns),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	branchesTable.SetStyles(filesStyle)

	// Initialize history table
	historyColumns := []table.Column{
		{Title: "Hash", Width: 10},
		{Title: "Message", Width: 50},
		{Title: "Author", Width: 20},
		{Title: "Date", Width: 15},
	}
	historyTable := table.New(
		table.WithColumns(historyColumns),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	historyTable.SetStyles(filesStyle)

	m.filesTable = filesTable
	m.suggestionsTable = suggestionsTable
	m.branchesTable = branchesTable
	m.historyTable = historyTable

	// Initialize custom input
	m.customInput = textinput.New()
	m.customInput.Placeholder = "Enter your custom commit message..."
	m.customInput.CharLimit = 200

	// Initialize edit input
	m.editInput = textinput.New()
	m.editInput.Placeholder = "Edit commit message..."
	m.editInput.CharLimit = 200

	// Initialize branch input
	m.branchInput = textinput.New()
	m.branchInput.Placeholder = "Enter new branch name..."
	m.branchInput.CharLimit = 100

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func findGitRepo() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func initGitRepo() error {
	cmd := exec.Command("git", "init")
	_, err := cmd.CombinedOutput()
	return err
}

// executeGitCommand runs a git command with retry logic to handle index.lock conflicts
func executeGitCommand(repoPath string, args ...string) ([]byte, error) {
	maxRetries := 3
	retryDelay := 100 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Check for index.lock file before attempting operation
		lockFile := filepath.Join(repoPath, ".git", "index.lock")
		if _, err := os.Stat(lockFile); err == nil {
			// Lock file exists, wait and retry
			time.Sleep(retryDelay)
			continue
		}

		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}

		output, err := cmd.CombinedOutput()

		// Check if error is due to index.lock
		if err != nil && strings.Contains(string(output), "index.lock") {
			// Wait before retry
			time.Sleep(retryDelay)
			retryDelay *= 2 // Exponential backoff
			continue
		}

		return output, err
	}

	return nil, fmt.Errorf("git command failed after %d retries: index.lock conflict", maxRetries)
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.SetWindowTitle("Git Commit Helper"),
		m.loadGitChanges(),
		m.loadGitStatus(),
		m.checkHookStatusOnStartup(),
	)
}

func (m model) checkHookStatusOnStartup() tea.Cmd {
	return func() tea.Msg {
		hookPath := filepath.Join(m.repoPath, ".git", "hooks", "commit-msg")
		if _, err := os.Stat(hookPath); os.IsNotExist(err) {
			return statusMsg{message: "💡 Tip: Press 'h' to install commit message validation hook"}
		}
		return statusMsg{message: "🔒 Commit validation hook is active"}
	}
}

func (m model) loadGitChanges() tea.Cmd {
	return func() tea.Msg {
		changes, err := getGitChanges(m.repoPath)
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to load changes: %v", err)}
		}
		return gitChangesMsg(changes)
	}
}

func (m model) generateSuggestions() tea.Cmd {
	return func() tea.Msg {
		suggestions := analyzeChangesForCommits(m.changes)
		return commitSuggestionsMsg(suggestions)
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case statusMsg:
		m.statusMsg = msg.message
		m.statusExpiry = time.Now().Add(3 * time.Second)
		return m, nil

	case gitChangesMsg:
		m.changes = []GitChange(msg)

		// Update files table
		m.updateFilesTable()

		// Auto-generate suggestions
		cmds = append(cmds, m.generateSuggestions())

		// Only refresh git status if enough time has passed (debounce)
		if time.Since(m.lastStatusUpdate) > 2*time.Second {
			cmds = append(cmds, m.loadGitStatus())
			m.lastStatusUpdate = time.Now()
		}

		m.statusMsg = fmt.Sprintf("✅ Loaded %d changed files", len(m.changes))
		m.statusExpiry = time.Now().Add(3 * time.Second)

		return m, tea.Batch(cmds...)

	case gitStatusMsg:
		m.gitState = GitStatus(msg)
		return m, nil

	case pushOutputMsg:
		m.pushOutput = msg.output
		m.lastCommit = msg.commit
		m.state = "output"
		m.statusMsg = "✅ Push completed - check tab 4 for details"
		m.statusExpiry = time.Now().Add(5 * time.Second)
		return m, nil

	case commitSuggestionsMsg:
		m.suggestions = []CommitSuggestion(msg)

		// Update suggestions table
		m.updateSuggestionsTable()

		m.statusMsg = fmt.Sprintf("🤖 Generated %d commit suggestions", len(m.suggestions))
		m.statusExpiry = time.Now().Add(3 * time.Second)

		return m, nil

	case branchesMsg:
		m.branches = []Branch(msg)
		m.updateBranchesTable()
		m.statusMsg = fmt.Sprintf("📋 Loaded %d branches", len(m.branches))
		m.statusExpiry = time.Now().Add(3 * time.Second)
		return m, nil

	case commitsMsg:
		m.commits = []Commit(msg)
		m.updateHistoryTable()
		m.statusMsg = fmt.Sprintf("📜 Loaded %d commits", len(m.commits))
		m.statusExpiry = time.Now().Add(3 * time.Second)
		return m, nil

	case diffMsg:
		m.diffContent = string(msg)
		m.state = "diff"
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		tableHeight := m.height - 8
		m.filesTable.SetHeight(tableHeight)
		m.suggestionsTable.SetHeight(tableHeight)
		m.adjustTableLayout()

		return m, nil

	case tea.KeyMsg:
		// Handle escape first for all states
		if msg.String() == "esc" {
			if m.state == "custom" {
				m.customInput.Blur()
				m.customInput.SetValue("")
				m.state = "files"
			} else if m.state == "edit" {
				m.editInput.Blur()
				m.editInput.SetValue("")
				m.state = "suggestions"
			} else if m.state == "newbranch" {
				m.branchInput.Blur()
				m.branchInput.SetValue("")
				m.state = "branches"
			} else if m.state == "diff" {
				m.state = "files"
			}
			return m, nil
		}

		// Handle enter for input states
		if msg.String() == "enter" {
			switch m.state {
			case "suggestions":
				if len(m.suggestions) > 0 {
					selectedIndex := m.suggestionsTable.Cursor()
					if selectedIndex < len(m.suggestions) {
						suggestion := m.suggestions[selectedIndex]
						return m, m.commitWithMessage(suggestion.Message)
					}
				}
			case "branches":
				if len(m.branches) > 0 {
					selectedIndex := m.branchesTable.Cursor()
					if selectedIndex < len(m.branches) {
						branch := m.branches[selectedIndex]
						if !branch.IsCurrent {
							return m, m.switchBranch(branch.Name)
						}
					}
				}
			case "newbranch":
				if m.branchInput.Value() != "" {
					branchName := m.branchInput.Value()
					m.branchInput.SetValue("")
					m.branchInput.Blur()
					m.state = "branches"
					return m, m.createBranch(branchName)
				}
			case "custom":
				if m.customInput.Value() != "" {
					msg := m.customInput.Value()
					// Clear the input and go back to files mode after commit
					m.customInput.SetValue("")
					m.customInput.Blur()
					m.state = "files"

					if !m.validateCommitMessage(msg) {
						// Show warning but still allow commit
						return m, tea.Batch(
							m.commitWithMessage(msg),
							m.refreshAfterCommit(),
							func() tea.Msg {
								return statusMsg{message: "⚠️ Commit message doesn't follow conventional format"}
							},
						)
					}
					return m, tea.Batch(
						m.commitWithMessage(msg),
						m.refreshAfterCommit(),
					)
				}
			case "edit":
				if m.editInput.Value() != "" {
					msg := m.editInput.Value()
					if !m.validateCommitMessage(msg) {
						// Show warning but still allow commit
						return m, tea.Batch(
							m.commitWithMessage(msg),
							m.refreshAfterCommit(),
							func() tea.Msg {
								return statusMsg{message: "⚠️ Commit message doesn't follow conventional format"}
							},
						)
					}
					return m, tea.Batch(
						m.commitWithMessage(msg),
						m.refreshAfterCommit(),
					)
				}
			}
			return m, nil
		}

		// Only handle other keys if NOT in text input mode
		if m.state != "custom" && m.state != "edit" && m.state != "newbranch" {
			switch msg.String() {
			case "q", "ctrl+c":
				if m.confirmAction != "" {
					m.confirmAction = ""
					m.statusMsg = "❌ Action cancelled"
					m.statusExpiry = time.Now().Add(2 * time.Second)
					return m, nil
				}
				return m, tea.Quit

			case "1":
				m.state = "files"
				return m, nil

			case "2":
				if len(m.suggestions) > 0 {
					m.state = "suggestions"
				}
				return m, nil

			case "3":
				m.state = "custom"
				m.customInput.Focus()
				return m, nil

			case "4":
				m.state = "branches"
				return m, m.loadBranches()

			case "5":
				m.state = "history"
				return m, m.loadHistory()

			case "6":
				m.state = "output"
				return m, nil

			case "e":
				if m.state == "suggestions" && len(m.suggestions) > 0 {
					selectedIndex := m.suggestionsTable.Cursor()
					if selectedIndex < len(m.suggestions) {
						suggestion := m.suggestions[selectedIndex]
						m.state = "edit"
						m.editInput.SetValue(suggestion.Message)
						m.editInput.Focus()
					}
				}
				return m, nil

			case "r":
				// Reset the status update timer to allow immediate refresh
				m.lastStatusUpdate = time.Time{}
				return m, tea.Batch(
					m.loadGitChanges(),
					func() tea.Msg {
						return statusMsg{message: "🔄 Refreshing..."}
					},
				)

			case "a":
				// Reset timer to ensure immediate refresh
				m.lastStatusUpdate = time.Time{}
				return m, tea.Batch(
					m.gitAddAll(),
					m.refreshAfterCommit(),
				)

			case "p":
				return m, m.gitPush()

			case "l":
				return m, m.gitPull()

			case "s":
				return m, m.gitStatus()

			case " ": // Space bar
				if m.state == "files" && len(m.changes) > 0 {
					selectedIndex := m.filesTable.Cursor()
					if selectedIndex < len(m.changes) {
						return m, m.toggleStaging(m.changes[selectedIndex].File)
					}
				}
				return m, nil

			case "v":
				if m.state == "files" && len(m.changes) > 0 {
					selectedIndex := m.filesTable.Cursor()
					if selectedIndex < len(m.changes) {
						return m, m.viewDiff(m.changes[selectedIndex].File)
					}
				}
				return m, nil

			case "n":
				if m.state == "branches" {
					m.state = "newbranch"
					m.branchInput.Focus()
					return m, nil
				}
				return m, nil

			case "d":
				if m.state == "branches" && len(m.branches) > 0 {
					selectedIndex := m.branchesTable.Cursor()
					if selectedIndex < len(m.branches) {
						branch := m.branches[selectedIndex]
						if branch.IsCurrent {
							return m, func() tea.Msg {
								return statusMsg{message: "❌ Cannot delete current branch"}
							}
						}
						m.confirmAction = "delete-branch:" + branch.Name
						return m, func() tea.Msg {
							return statusMsg{message: fmt.Sprintf("⚠️ Press 'y' to confirm delete branch '%s', or 'q' to cancel", branch.Name)}
						}
					}
				}
				return m, nil

			case "y":
				if strings.HasPrefix(m.confirmAction, "delete-branch:") {
					branchName := strings.TrimPrefix(m.confirmAction, "delete-branch:")
					m.confirmAction = ""
					return m, m.deleteBranch(branchName)
				}
				return m, nil

			case "R":
				// Git reset (unstage all) - reset timer to ensure immediate refresh
				m.lastStatusUpdate = time.Time{}
				return m, tea.Batch(
					m.gitReset(),
					m.refreshAfterCommit(),
				)

			case "A":
				// Git commit --amend - reset timer to ensure immediate refresh
				m.lastStatusUpdate = time.Time{}
				return m, tea.Batch(
					m.gitAmend(),
					m.refreshAfterCommit(),
				)


			case "h":
				return m, m.generateCommitHook()

			case "H":
				return m, m.removeCommitHook()

			case "i":
				return m, m.checkHookStatus()

			case "?":
				// Show valid commit message format status
				return m, func() tea.Msg {
					return statusMsg{message: "Valid formats: feat(scope): description | fix: description | docs/test/chore: description"}
				}
			}
		}
	}

	// Update the appropriate component based on state
	switch m.state {
	case "files":
		m.filesTable, cmd = m.filesTable.Update(msg)
	case "suggestions":
		m.suggestionsTable, cmd = m.suggestionsTable.Update(msg)
	case "custom":
		m.customInput, cmd = m.customInput.Update(msg)
	case "edit":
		m.editInput, cmd = m.editInput.Update(msg)
	case "branches":
		m.branchesTable, cmd = m.branchesTable.Update(msg)
	case "newbranch":
		m.branchInput, cmd = m.branchInput.Update(msg)
	case "history":
		m.historyTable, cmd = m.historyTable.Update(msg)
	case "output", "diff":
		// Output/diff view doesn't need input handling
		break
	}

	return m, cmd
}

func (m model) View() string {
	var content string

	// Check hook status for display
	hookPath := filepath.Join(m.repoPath, ".git", "hooks", "commit-msg")
	hookStatus := ""
	if _, err := os.Stat(hookPath); err == nil {
		hookStatus = " 🔒"
	}

	// Create all header components
	title := titleStyle.Render("🚀 Git Commit Helper")
	repoInfo := repositoryStyle.Render(fmt.Sprintf(" Repository: %s%s", filepath.Base(m.repoPath), hookStatus))

	// Create git status bar
	gitStatusBar := m.renderGitStatusBar()

	// Create tabs
	tab1 := m.renderTab("1", "📁 Files", m.state == "files" || m.state == "diff")
	tab2 := m.renderTab("2", "💡 Suggestions", m.state == "suggestions")
	tab3 := m.renderTab("3", "✏️  Custom", m.state == "custom")
	tab4 := m.renderTab("4", "🌿 Branches", m.state == "branches" || m.state == "newbranch")
	tab5 := m.renderTab("5", "📜 History", m.state == "history")
	tab6 := m.renderTab("6", "📤 Output", m.state == "output")

	// Calculate spacing to keep everything on one line
	spacer := strings.Repeat(" ", 2)

	// Combine everything on one line
	fullHeader := lipgloss.JoinHorizontal(
		lipgloss.Top,
		title,
		repoInfo,
		spacer,
		tab1,
		tab2,
		tab3,
		tab4,
		tab5,
		tab6,
	)

	// Combine header with git status
	header := lipgloss.JoinVertical(
		lipgloss.Left,
		fullHeader,
		gitStatusBar,
	)

	// Content based on current state
	switch m.state {
	case "files":
		if len(m.changes) == 0 {
			content = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Render("No changes found. Run 'git add' to stage files or make some changes.")
		} else {
			content = m.filesTable.View()
		}

	case "suggestions":
		if len(m.suggestions) == 0 {
			content = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Render("No suggestions available. Please add some files first.")
		} else {
			content = m.suggestionsTable.View()
		}

	case "custom":
		inputLabel := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Render("Custom Commit Message:\nValid formats: feat(scope): description | fix: description | docs/test/chore: description")
		content = fmt.Sprintf("%s\n\n%s", inputLabel, m.customInput.View())

	case "edit":
		inputLabel := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Render("Edit Commit Message:")
		content = fmt.Sprintf("%s\n\n%s", inputLabel, m.editInput.View())

	case "branches":
		if len(m.branches) == 0 {
			content = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Render("Loading branches...")
		} else {
			content = m.branchesTable.View()
		}

	case "newbranch":
		inputLabel := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Render("Create New Branch:")
		content = fmt.Sprintf("%s\n\n%s", inputLabel, m.branchInput.View())

	case "history":
		if len(m.commits) == 0 {
			content = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Render("Loading commit history...")
		} else {
			content = m.historyTable.View()
		}

	case "diff":
		if m.diffContent != "" {
			diffLabel := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("86")).
				Render("File Diff (press ESC to go back):")
			content = fmt.Sprintf("%s\n\n%s", diffLabel, m.diffContent)
		} else {
			content = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Render("No diff to display.")
		}

	case "output":
		if m.pushOutput != "" {
			outputLabel := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("86")).
				Render("Git Push Output:")
			commitLabel := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("208")).
				Render("Last Commit:")
			content = fmt.Sprintf("%s\n\n%s\n\n%s\n%s", outputLabel, m.pushOutput, commitLabel, m.lastCommit)
		} else {
			content = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Render("No push output available. Use 'p' to push changes.")
		}
	}

	// Footer with help and status
	footer := m.renderFooter()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		content,
		"",
		helpStyle.Render(footer),
	)
}

func (m model) renderTab(key, label string, active bool) string {
	style := lipgloss.NewStyle().Padding(0, 2)

	if active {
		style = style.
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Background(lipgloss.Color("240"))
	} else {
		style = style.Foreground(lipgloss.Color("240"))
	}

	return style.Render(fmt.Sprintf("[%s] %s", key, label))
}

func (m model) renderFooter() string {
	// Color styles for footer
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))     // Blue color for keys
	actionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("86"))  // Green color for action text
	bulletStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // Gray color for bullets

	var footer string
	switch m.state {
	case "files":
		footer = fmt.Sprintf("%s: %s %s %s: %s %s %s: %s %s %s: %s %s %s: %s\n%s: %s %s %s: %s %s %s: %s %s %s: %s",
			keyStyle.Render("1-6"), actionStyle.Render("tabs"), bulletStyle.Render("•"),
			keyStyle.Render("space"), actionStyle.Render("stage/unstage"), bulletStyle.Render("•"),
			keyStyle.Render("v"), actionStyle.Render("view diff"), bulletStyle.Render("•"),
			keyStyle.Render("a"), actionStyle.Render("add all"), bulletStyle.Render("•"),
			keyStyle.Render("r"), actionStyle.Render("refresh"),
			keyStyle.Render("p/l"), actionStyle.Render("push/pull"), bulletStyle.Render("•"),
			keyStyle.Render("R/A"), actionStyle.Render("reset/amend"), bulletStyle.Render("•"),
			keyStyle.Render("h/H"), actionStyle.Render("hooks"), bulletStyle.Render("•"),
			keyStyle.Render("q"), actionStyle.Render("quit"))
	case "suggestions":
		footer = fmt.Sprintf("%s: %s %s %s: %s %s %s: %s %s %s: %s %s %s: %s %s %s: %s\n%s: %s %s %s: %s %s %s: %s",
			keyStyle.Render("1-6"), actionStyle.Render("tabs"), bulletStyle.Render("•"),
			keyStyle.Render("↑↓"), actionStyle.Render("navigate"), bulletStyle.Render("•"),
			keyStyle.Render("enter"), actionStyle.Render("commit"), bulletStyle.Render("•"),
			keyStyle.Render("e"), actionStyle.Render("edit"), bulletStyle.Render("•"),
			keyStyle.Render("a/R/A"), actionStyle.Render("add/reset/amend"), bulletStyle.Render("•"),
			keyStyle.Render("p"), actionStyle.Render("push"),
			keyStyle.Render("h/H"), actionStyle.Render("hooks"), bulletStyle.Render("•"),
			keyStyle.Render("i/?"), actionStyle.Render("info"), bulletStyle.Render("•"),
			keyStyle.Render("q"), actionStyle.Render("quit"))
	case "custom":
		footer = fmt.Sprintf("%s: %s %s %s: %s",
			keyStyle.Render("enter"), actionStyle.Render("commit"), bulletStyle.Render("•"),
			keyStyle.Render("esc"), actionStyle.Render("cancel"))
	case "edit":
		footer = fmt.Sprintf("%s: %s %s %s: %s",
			keyStyle.Render("enter"), actionStyle.Render("commit"), bulletStyle.Render("•"),
			keyStyle.Render("esc"), actionStyle.Render("back to suggestions"))
	case "branches":
		footer = fmt.Sprintf("%s: %s %s %s: %s %s %s: %s %s %s: %s %s %s: %s",
			keyStyle.Render("1-6"), actionStyle.Render("tabs"), bulletStyle.Render("•"),
			keyStyle.Render("enter"), actionStyle.Render("switch"), bulletStyle.Render("•"),
			keyStyle.Render("n"), actionStyle.Render("new branch"), bulletStyle.Render("•"),
			keyStyle.Render("d"), actionStyle.Render("delete"), bulletStyle.Render("•"),
			keyStyle.Render("q"), actionStyle.Render("quit"))
	case "newbranch":
		footer = fmt.Sprintf("%s: %s %s %s: %s",
			keyStyle.Render("enter"), actionStyle.Render("create"), bulletStyle.Render("•"),
			keyStyle.Render("esc"), actionStyle.Render("cancel"))
	case "history":
		footer = fmt.Sprintf("%s: %s %s %s: %s",
			keyStyle.Render("1-6"), actionStyle.Render("tabs"), bulletStyle.Render("•"),
			keyStyle.Render("q"), actionStyle.Render("quit"))
	case "diff":
		footer = fmt.Sprintf("%s: %s %s %s: %s",
			keyStyle.Render("esc"), actionStyle.Render("back to files"), bulletStyle.Render("•"),
			keyStyle.Render("q"), actionStyle.Render("quit"))
	case "output":
		footer = fmt.Sprintf("%s: %s %s %s: %s",
			keyStyle.Render("1-6"), actionStyle.Render("switch tabs"), bulletStyle.Render("•"),
			keyStyle.Render("q"), actionStyle.Render("quit"))
	}

	// Add status message if present
	if m.statusMsg != "" && time.Now().Before(m.statusExpiry) {
		var statusColor lipgloss.Color = "86"
		if strings.Contains(m.statusMsg, "❌") {
			statusColor = "196"
		}
		statusLine := lipgloss.NewStyle().
			Foreground(statusColor).
			Bold(true).
			Render(" > " + m.statusMsg)
		footer = footer + "\n" + statusLine
	}

	return footer
}

func (m model) renderGitStatusBar() string {
	// Status indicators
	cleanIcon := "✅"
	dirtyIcon := "🔴"
	aheadIcon := "⬆️"
	behindIcon := "⬇️"
	stagedIcon := "🟢"
	emptyIcon := "⚪"

	// Branch info
	branchStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	branchInfo := branchStyle.Render(fmt.Sprintf("Branch: %s", m.gitState.Branch))

	// Staging status - always show this
	var stagingStatus string
	if m.gitState.StagedFiles == 0 {
		stagingStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(fmt.Sprintf("%s Nothing staged", emptyIcon))
	} else {
		stagingStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Render(fmt.Sprintf("%s %d files staged", stagedIcon, m.gitState.StagedFiles))
	}

	// Working directory status - only show dirty if there are unstaged files
	var workingDirStatus string
	if m.gitState.UnstagedFiles > 0 {
		workingDirStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(fmt.Sprintf("%s WD dirty (%d files)", dirtyIcon, m.gitState.UnstagedFiles))
	} else {
		workingDirStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Render(fmt.Sprintf("%s WD clean", cleanIcon))
	}

	// Ahead/behind info
	var syncInfo []string
	if m.gitState.Ahead > 0 {
		syncInfo = append(syncInfo, lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Render(fmt.Sprintf("%s %d ahead", aheadIcon, m.gitState.Ahead)))
	}
	if m.gitState.Behind > 0 {
		syncInfo = append(syncInfo, lipgloss.NewStyle().Foreground(lipgloss.Color("173")).Render(fmt.Sprintf("%s %d behind", behindIcon, m.gitState.Behind)))
	}

	// Combine all elements - always show branch, staging, and working dir status
	elements := []string{branchInfo, stagingStatus, workingDirStatus}
	elements = append(elements, syncInfo...)

	return lipgloss.NewStyle().Background(lipgloss.Color("235")).Padding(0, 1).Render(strings.Join(elements, " • "))
}

func (m model) gitAddAll() tea.Cmd {
	return func() tea.Msg {
		output, err := executeGitCommand(m.repoPath, "add", ".")
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Git add failed: %v - %s", err, string(output))}
		}

		return statusMsg{message: "✅ Added all file(s) to staging"}
	}
}

func (m model) gitPush() tea.Cmd {
	return func() tea.Msg {
		// Check if there are commits to push
		statusCmd := exec.Command("git", "status", "--porcelain=v1", "--branch")
		statusCmd.Dir = m.repoPath
		statusOutput, err := statusCmd.Output()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to check git status: %v", err)}
		}

		statusStr := string(statusOutput)
		if !strings.Contains(statusStr, "ahead") {
			return statusMsg{message: "ℹ️ No commits to push"}
		}

		// Get last commit info before push
		commitCmd := exec.Command("git", "log", "-1", "--oneline")
		commitCmd.Dir = m.repoPath
		commitOutput, _ := commitCmd.Output()
		lastCommit := strings.TrimSpace(string(commitOutput))

		// Get list of changed files in last commit
		filesCmd := exec.Command("git", "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")
		filesCmd.Dir = m.repoPath
		filesOutput, _ := filesCmd.Output()
		changedFiles := strings.TrimSpace(string(filesOutput))

		cmd := exec.Command("git", "push")
		cmd.Dir = m.repoPath
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}

		output, err := cmd.CombinedOutput()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Git push failed: %v - %s", err, string(output))}
		}

		// Format detailed output
		detailedOutput := fmt.Sprintf("Push Output:\n%s\n\nLast Commit:\n%s\n\nChanged Files:\n%s", string(output), lastCommit, changedFiles)

		return pushOutputMsg{
			output: detailedOutput,
			commit: lastCommit,
		}
	}
}

func (m model) gitStatus() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "status", "--short")
		cmd.Dir = m.repoPath
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}

		output, err := cmd.CombinedOutput()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Git status failed: %v", err)}
		}

		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		if len(lines) == 1 && lines[0] == "" {
			return statusMsg{message: "✅ Working tree clean"}
		}

		return statusMsg{message: fmt.Sprintf("📊 %d files modified", len(lines))}
	}
}

// Git operation functions
func (m model) commitWithMessage(message string) tea.Cmd {
	return func() tea.Msg {
		// Check if there are staged changes
		statusCmd := exec.Command("git", "diff", "--cached", "--name-only")
		statusCmd.Dir = m.repoPath
		statusOutput, err := statusCmd.Output()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to check staged changes: %v", err)}
		}

		if len(strings.TrimSpace(string(statusOutput))) == 0 {
			return statusMsg{message: "❌ No staged changes to commit. Use 'a' to stage files first."}
		}

		_, err = executeGitCommand(m.repoPath, "commit", "-m", message)
		if err != nil {
			return statusMsg{message: "❌ Commit failed - Valid formats: feat(scope): description | fix: description | docs/test/chore: description"}
		}

		return statusMsg{message: fmt.Sprintf("✅ Committed: %s", message)}
	}
}

func parseGitStatusOutput(statusText string) (stagedFiles, unstagedFiles int, clean bool) {
	// Debug logging to file
	f, _ := os.OpenFile("/tmp/git-debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	defer f.Close()
	fmt.Fprintf(f, "\n=== parseGitStatusOutput called ===\n")
	fmt.Fprintf(f, "Raw status text: %q\n", statusText)

	clean = statusText == ""
	if clean {
		return 0, 0, true
	}

	lines := strings.Split(statusText, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) >= 2 {
			stagedStatus := line[0]
			unstagedStatus := line[1]

			fmt.Fprintf(f, "Line %d: %q -> staged='%c' unstaged='%c'\n", i, line, stagedStatus, unstagedStatus)

			// Count staged files: first column shows staged changes (not space, not untracked)
			if stagedStatus != ' ' && stagedStatus != '?' {
				stagedFiles++
				fmt.Fprintf(f, "  -> Counted as STAGED (total: %d)\n", stagedFiles)
			}

			// Count unstaged files: second column shows unstaged changes
			if unstagedStatus != ' ' {
				unstagedFiles++
				fmt.Fprintf(f, "  -> Counted as UNSTAGED (total: %d)\n", unstagedFiles)
			}
		}
	}

	fmt.Fprintf(f, "Final count: staged=%d, unstaged=%d\n", stagedFiles, unstagedFiles)
	return stagedFiles, unstagedFiles, false
}

// getBranchName returns the current git branch name
func getBranchName(repoPath string) string {
	branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = repoPath
	branchOutput, err := branchCmd.Output()
	if err == nil {
		return strings.TrimSpace(string(branchOutput))
	}
	return "unknown"
}

// getAheadBehindCount returns ahead/behind counts relative to upstream
func getAheadBehindCount(repoPath string) (ahead, behind int) {
	aheadBehindCmd := exec.Command("git", "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	aheadBehindCmd.Dir = repoPath
	aheadBehindOutput, err := aheadBehindCmd.Output()
	if err == nil {
		parts := strings.Fields(strings.TrimSpace(string(aheadBehindOutput)))
		if len(parts) == 2 {
			if a, err := strconv.Atoi(parts[0]); err == nil {
				ahead = a
			}
			if b, err := strconv.Atoi(parts[1]); err == nil {
				behind = b
			}
		}
	}
	return ahead, behind
}

func (m model) loadGitStatus() tea.Cmd {
	return func() tea.Msg {
		status := GitStatus{}

		// Get branch name
		status.Branch = getBranchName(m.repoPath)

		// Check status and count files
		statusCmd := exec.Command("git", "status", "--porcelain")
		statusCmd.Dir = m.repoPath
		statusOutput, err := statusCmd.Output()
		if err == nil {
			statusText := string(statusOutput)

			status.StagedFiles, status.UnstagedFiles, status.Clean = parseGitStatusOutput(statusText)
		}

		// Check ahead/behind status
		status.Ahead, status.Behind = getAheadBehindCount(m.repoPath)

		return gitStatusMsg(status)
	}
}

func (m model) gitReset() tea.Cmd {
	return func() tea.Msg {
		// Check current status to count staged files
		statusCmd := exec.Command("git", "status", "--porcelain")
		statusCmd.Dir = m.repoPath
		statusOutput, err := statusCmd.Output()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to check git status: %v", err)}
		}

		statusText := strings.TrimSpace(string(statusOutput))
		stagedCount, _, _ := parseGitStatusOutput(statusText)

		if stagedCount == 0 {
			return statusMsg{message: "ℹ️ No staged changes to reset"}
		}

		// Reset all staged files
		output, err := executeGitCommand(m.repoPath, "reset", "HEAD")
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Git reset failed: %v - %s", err, string(output))}
		}
		return statusMsg{message: fmt.Sprintf("✅ Reset %d staged file(s)", stagedCount)}
	}
}

func (m model) gitAmend() tea.Cmd {
	return func() tea.Msg {

		// Get the current commit message
		msgCmd := exec.Command("git", "log", "-1", "--pretty=%B")
		msgCmd.Dir = m.repoPath
		msgOutput, err := msgCmd.Output()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to get commit message: %v", err)}
		}

		currentMsg := strings.TrimSpace(string(msgOutput))

		// Check if there are staged changes to include
		stagedCmd := exec.Command("git", "diff", "--cached", "--name-only")
		stagedCmd.Dir = m.repoPath
		stagedOutput, err := stagedCmd.Output()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to check staged changes: %v", err)}
		}

		args := []string{"commit", "--amend"}
		if len(strings.TrimSpace(string(stagedOutput))) == 0 {
			// No staged changes, just amend message
			args = append(args, "--no-edit")
			return statusMsg{message: "ℹ️ No staged changes to amend. Use 'a' to stage files or edit commit message manually."}
		}

		// Amend with staged changes, keeping the same message
		args = append(args, "-m", currentMsg)

		cmd := exec.Command("git", args...)
		cmd.Dir = m.repoPath
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}

		output, err := cmd.CombinedOutput()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Git amend failed: %v - %s", err, string(output))}
		}

		return statusMsg{message: "✅ Commit amended with staged changes"}
	}
}

// Make sure this function forces immediate git status refresh
func (m *model) refreshAfterCommit() tea.Cmd {
	// Reset status update timer to allow immediate refresh after operations
	m.lastStatusUpdate = time.Time{}
	return func() tea.Msg {
		// Small delay to ensure git index is fully updated before checking status
		time.Sleep(50 * time.Millisecond)

		// Force immediate status refresh by calling loadGitStatus directly
		return tea.Batch(
			m.loadGitStatus(),
			m.loadGitChanges(),
		)()
	}
}

func getGitChanges(repoPath string) ([]GitChange, error) {
	output, err := executeGitCommand(repoPath, "status", "--porcelain")
	if err != nil {
		return nil, err
	}

	var changes []GitChange
	lines := strings.Split(string(output), "\n")

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		if len(line) < 3 {
			continue
		}

		status := (line[:2])
		file := strings.TrimSpace(line[3:])

		change := GitChange{
			File:   file,
			Status: status,
		}

		changes = append(changes, change)
	}

	return changes, nil
}

func analyzeChangesForCommits(changes []GitChange) []CommitSuggestion {
	var suggestions []CommitSuggestion

	// Generate individual suggestions
	individualSuggestions := []CommitSuggestion{}
	for _, change := range changes {
		diffInfo := getFileDiff(change.File)
		analysis := analyzeFileChange(change, diffInfo)

		suggestion := CommitSuggestion{
			Type:    analysis.Type,
			Message: analysis.Message,
		}
		individualSuggestions = append(individualSuggestions, suggestion)
	}

	// Create combined suggestion as first option
	combinedSuggestion := generateCombinedSuggestion(individualSuggestions, nil)
	if combinedSuggestion.Message != "" {
		suggestions = append(suggestions, combinedSuggestion)
	}

	// Only add individual suggestions if there are 3 or fewer files
	if len(individualSuggestions) <= 3 {
		suggestions = append(suggestions, individualSuggestions...)
	}

	// Limit to max 5 suggestions total
	if len(suggestions) > 5 {
		suggestions = suggestions[:5]
	}

	return suggestions
}

func generateCombinedSuggestion(individual []CommitSuggestion, grouped []CommitSuggestion) CommitSuggestion {
	if len(individual) == 0 {
		return CommitSuggestion{}
	}

	// Count types to determine the main focus
	typeCounts := make(map[string]int)
	scopeCounts := make(map[string]int)

	for _, suggestion := range individual {
		typeCounts[suggestion.Type]++
		// Extract scope from message if formatted conventionally
		if strings.Contains(suggestion.Message, "(") && strings.Contains(suggestion.Message, "):") {
			start := strings.Index(suggestion.Message, "(") + 1
			end := strings.Index(suggestion.Message, "):")
			if end > start {
				scope := suggestion.Message[start:end]
				scopeCounts[scope]++
			}
		}
	}

	// Find the most common type and scope
	var mainType, mainScope string
	maxTypeCount := 0
	for commitType, count := range typeCounts {
		if count > maxTypeCount {
			maxTypeCount = count
			mainType = commitType
		}
	}

	maxScopeCount := 0
	for scope, count := range scopeCounts {
		if count > maxScopeCount {
			maxScopeCount = count
			mainScope = scope
		}
	}

	// Generate a simple combined message
	var description string
	totalFiles := len(individual)

	if len(typeCounts) == 1 {
		// All changes are the same type - keep it simple
		switch mainType {
		case "feat":
			description = "add features"
		case "fix":
			description = "fix issues"
		case "docs":
			description = "update docs"
		case "test":
			description = "update tests"
		case "chore":
			description = "update config"
		case "refactor":
			description = "refactor code"
		default:
			description = "update files"
		}
	} else {
		// Mixed types - just use "update" for simplicity
		description = "update multiple files"
	}

	// Only add file count if more than 1 file
	if totalFiles > 1 {
		description = fmt.Sprintf("%s (%d files)", description, totalFiles)
	}

	// Use the main scope if it represents majority of changes
	finalScope := ""
	if maxScopeCount > totalFiles/2 {
		finalScope = mainScope
	}

	message := formatConventionalCommit(mainType, finalScope, description)

	return CommitSuggestion{
		Type:    mainType,
		Message: message,
	}
}

type FileAnalysis struct {
	Type    string
	Message string
	Scope   string
}

type DiffInfo struct {
	LinesAdded   int
	LinesRemoved int
	Functions    []string
	Imports      []string
	HasTests     bool
	HasDocs      bool
}

func getFileDiff(filePath string) DiffInfo {
	cmd := exec.Command("git", "diff", "--cached", filePath)
	output, err := cmd.Output()
	if err != nil {
		// Try unstaged diff if no staged changes
		cmd = exec.Command("git", "diff", filePath)
		output, _ = cmd.Output()
	}

	return parseDiffOutput(string(output))
}

func parseDiffOutput(diff string) DiffInfo {
	info := DiffInfo{}
	lines := strings.Split(diff, "\n")

	for _, line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			info.LinesAdded++

			// Detect function definitions (enhanced patterns for multiple languages)
			funcName := extractFunctionName(line)
			if funcName != "" && !contains(info.Functions, funcName) {
				info.Functions = append(info.Functions, funcName)
			}

			// Detect imports/includes
			if isImportLine(line) {
				importName := extractImportName(line)
				if importName != "" && !contains(info.Imports, importName) {
					info.Imports = append(info.Imports, importName)
				}
			}

			// Detect test-related content
			if strings.Contains(strings.ToLower(line), "test") {
				info.HasTests = true
			}

			// Detect documentation
			if strings.Contains(line, "//") || strings.Contains(line, "/*") ||
				strings.Contains(line, "/**") || strings.Contains(line, "#") {
				info.HasDocs = true
			}
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			info.LinesRemoved++
		}
	}

	return info
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func isImportLine(line string) bool {
	trimmed := strings.TrimSpace(line[1:]) // Remove the + prefix
	return strings.HasPrefix(trimmed, "import ") ||
		strings.HasPrefix(trimmed, "from ") ||
		strings.HasPrefix(trimmed, "#include") ||
		strings.HasPrefix(trimmed, "require(") ||
		strings.HasPrefix(trimmed, "const ") && strings.Contains(trimmed, "require(") ||
		strings.HasPrefix(trimmed, "use ")
}

func extractImportName(line string) string {
	trimmed := strings.TrimSpace(line[1:]) // Remove the + prefix

	// Go imports
	if strings.HasPrefix(trimmed, "import ") {
		parts := strings.Fields(trimmed)
		if len(parts) >= 2 {
			importPath := strings.Trim(parts[len(parts)-1], "\"")
			if strings.Contains(importPath, "/") {
				parts := strings.Split(importPath, "/")
				return parts[len(parts)-1]
			}
			return importPath
		}
	}

	// Python imports
	if strings.HasPrefix(trimmed, "from ") {
		parts := strings.Fields(trimmed)
		if len(parts) >= 2 {
			return parts[1]
		}
	}

	// JavaScript/Node requires
	if strings.Contains(trimmed, "require(") {
		start := strings.Index(trimmed, "require(") + 8
		end := strings.Index(trimmed[start:], ")")
		if end > 0 {
			pkg := strings.Trim(trimmed[start:start+end], "\"'")
			if strings.Contains(pkg, "/") {
				parts := strings.Split(pkg, "/")
				return parts[len(parts)-1]
			}
			return pkg
		}
	}

	return ""
}

func extractFunctionName(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "+") {
		trimmed = strings.TrimSpace(trimmed[1:])
	}

	// Go function detection
	if strings.Contains(trimmed, "func ") {
		idx := strings.Index(trimmed, "func ")
		remaining := trimmed[idx+5:]

		// Handle receiver methods like "func (r *Receiver) Method("
		if strings.HasPrefix(remaining, "(") {
			parenEnd := strings.Index(remaining, ")")
			if parenEnd > 0 {
				remaining = strings.TrimSpace(remaining[parenEnd+1:])
			}
		}

		parts := strings.Fields(remaining)
		if len(parts) > 0 {
			name := parts[0]
			if parenIdx := strings.Index(name, "("); parenIdx != -1 {
				name = name[:parenIdx]
			}
			return name
		}
	}

	// JavaScript/TypeScript function detection
	if strings.Contains(trimmed, "function ") {
		idx := strings.Index(trimmed, "function ")
		parts := strings.Fields(trimmed[idx:])
		if len(parts) > 1 {
			name := parts[1]
			if parenIdx := strings.Index(name, "("); parenIdx != -1 {
				name = name[:parenIdx]
			}
			return name
		}
	}

	// Arrow function detection
	if strings.Contains(trimmed, " => ") || strings.Contains(trimmed, "=>") {
		// Look for patterns like "const funcName = " or "export const funcName = "
		if strings.Contains(trimmed, "const ") {
			idx := strings.Index(trimmed, "const ")
			remaining := trimmed[idx+6:]
			parts := strings.Fields(remaining)
			if len(parts) > 0 {
				name := parts[0]
				if strings.Contains(name, "=") {
					name = strings.Split(name, "=")[0]
				}
				return strings.TrimSpace(name)
			}
		}
	}

	// Python function detection
	if strings.Contains(trimmed, "def ") {
		idx := strings.Index(trimmed, "def ")
		parts := strings.Fields(trimmed[idx:])
		if len(parts) > 1 {
			name := parts[1]
			if parenIdx := strings.Index(name, "("); parenIdx != -1 {
				name = name[:parenIdx]
			}
			return name
		}
	}

	// Class detection
	if strings.Contains(trimmed, "class ") {
		idx := strings.Index(trimmed, "class ")
		parts := strings.Fields(trimmed[idx:])
		if len(parts) > 1 {
			name := parts[1]
			// Remove inheritance syntax
			if colonIdx := strings.Index(name, ":"); colonIdx != -1 {
				name = name[:colonIdx]
			}
			if parenIdx := strings.Index(name, "("); parenIdx != -1 {
				name = name[:parenIdx]
			}
			if braceIdx := strings.Index(name, "{"); braceIdx != -1 {
				name = name[:braceIdx]
			}
			return name
		}
	}

	// Method detection (for languages like Java, C#)
	if strings.Contains(trimmed, "public ") || strings.Contains(trimmed, "private ") ||
		strings.Contains(trimmed, "protected ") || strings.Contains(trimmed, "static ") {
		parts := strings.Fields(trimmed)
		for i, part := range parts {
			if strings.Contains(part, "(") {
				methodName := strings.Split(part, "(")[0]
				// Check if this looks like a method name (not a type)
				if i > 0 && !strings.Contains(methodName, ".") && methodName != "" {
					return methodName
				}
			}
		}
	}

	return ""
}

func analyzeFileChange(change GitChange, diff DiffInfo) FileAnalysis {
	file := change.File
	status := change.Status

	// Determine scope and type based on file path and content
	scope := determineScope(file)
	commitType := determineAdvancedCommitType(file, status, diff)
	rawMessage := generateSmartCommitMessage(file, status, diff, commitType)

	// Format as conventional commit
	message := formatConventionalCommit(commitType, scope, rawMessage)

	return FileAnalysis{
		Type:    commitType,
		Message: message,
		Scope:   scope,
	}
}

func determineAdvancedCommitType(file, status string, diff DiffInfo) string {
	fileName := filepath.Base(file)

	// New files
	if strings.Contains(status, "A") {
		if strings.Contains(file, "test") || strings.HasSuffix(file, "_test.go") {
			return "test"
		}
		if strings.HasSuffix(file, ".md") || strings.Contains(file, "README") {
			return "docs"
		}
		if len(diff.Functions) > 0 {
			return "feat"
		}
		return "feat"
	}

	// Deleted files
	if strings.Contains(status, "D") {
		return "chore"
	}

	// Modified files - analyze the changes
	if strings.Contains(status, "M") {
		// Documentation changes
		if strings.HasSuffix(file, ".md") || strings.Contains(file, "README") || strings.Contains(file, "doc") {
			return "docs"
		}

		// Test files
		if strings.Contains(file, "test") || strings.HasSuffix(file, "_test.go") || strings.HasSuffix(file, ".test.js") {
			return "test"
		}

		// Configuration files
		if strings.Contains(file, "config") || strings.HasSuffix(file, ".json") ||
			strings.HasSuffix(file, ".yaml") || strings.HasSuffix(file, ".yml") ||
			strings.HasSuffix(file, ".toml") || fileName == "Dockerfile" ||
			fileName == "Makefile" || strings.HasSuffix(file, ".env") {
			return "chore"
		}

		// Package management
		if fileName == "package.json" || fileName == "go.mod" || fileName == "requirements.txt" ||
			fileName == "Cargo.toml" || fileName == "pom.xml" {
			if len(diff.Imports) > 0 {
				return "feat" // Adding new dependencies
			}
			return "chore"
		}

		// Bug fixes - look for keywords in diff
		if containsBugFixKeywords(diff) {
			return "fix"
		}

		// New functionality
		if len(diff.Functions) > 0 || diff.LinesAdded > diff.LinesRemoved*2 {
			return "feat"
		}

		// Performance or refactoring
		if diff.LinesAdded > 0 && diff.LinesRemoved > 0 &&
			abs(diff.LinesAdded-diff.LinesRemoved) < 10 {
			return "refactor"
		}

		// Small changes/fixes
		if diff.LinesAdded+diff.LinesRemoved < 10 {
			return "fix"
		}

		return "feat"
	}

	return "chore"
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func containsBugFixKeywords(diff DiffInfo) bool {
	// This would need to analyze the actual diff content for keywords
	// For now, we'll use a simple heuristic
	return diff.LinesRemoved > 0 && diff.LinesAdded < diff.LinesRemoved
}

func generateSmartCommitMessage(file, status string, diff DiffInfo, commitType string) string {
	fileName := filepath.Base(file)
	fileExt := filepath.Ext(file)
	baseName := strings.TrimSuffix(fileName, fileExt)

	// Get directory context for better messages
	dir := filepath.Dir(file)
	dirName := filepath.Base(dir)

	switch status {
	case "A":
		// New file messages
		if len(diff.Functions) > 0 {
			if len(diff.Functions) == 1 {
				return fmt.Sprintf("add %s function", diff.Functions[0])
			}
			return fmt.Sprintf("add %s with %d functions", baseName, len(diff.Functions))
		}

		if len(diff.Imports) > 0 {
			return fmt.Sprintf("add %s with dependencies", baseName)
		}

		// Specific file type messages
		if strings.HasSuffix(fileName, "_test.go") || strings.Contains(fileName, "test") {
			return fmt.Sprintf("add tests for %s", strings.TrimSuffix(baseName, "_test"))
		}

		if strings.HasSuffix(fileName, ".md") {
			if fileName == "README.md" {
				return "add README documentation"
			}
			return fmt.Sprintf("add %s documentation", baseName)
		}

		if commitType == "chore" {
			return fmt.Sprintf("add %s config", baseName)
		}

		return fmt.Sprintf("add %s", fileName)

	case "D":
		return fmt.Sprintf("remove %s", fileName)

	case "M":
		// Modified file messages - be more specific
		if commitType == "docs" {
			if fileName == "README.md" {
				return "update README"
			}
			return fmt.Sprintf("update %s docs", baseName)
		}

		if commitType == "test" {
			testSubject := strings.TrimSuffix(baseName, "_test")
			return fmt.Sprintf("update %s tests", testSubject)
		}

		if commitType == "chore" {
			if fileName == "package.json" || fileName == "go.mod" {
				if len(diff.Imports) > 0 {
					return "update dependencies"
				}
				return "update package config"
			}
			return fmt.Sprintf("update %s config", baseName)
		}

		// Function-specific messages
		if len(diff.Functions) > 0 {
			if len(diff.Functions) == 1 {
				funcName := diff.Functions[0]
				if commitType == "fix" {
					return fmt.Sprintf("fix %s function", funcName)
				} else if commitType == "refactor" {
					return fmt.Sprintf("refactor %s function", funcName)
				}
				return fmt.Sprintf("update %s function", funcName)
			} else if len(diff.Functions) <= 3 {
				if commitType == "refactor" {
					return fmt.Sprintf("refactor %d functions in %s", len(diff.Functions), baseName)
				}
				return fmt.Sprintf("update %d functions in %s", len(diff.Functions), baseName)
			}
		}

		// Import changes
		if len(diff.Imports) > 0 {
			if commitType == "feat" {
				return fmt.Sprintf("add dependencies to %s", baseName)
			}
			return fmt.Sprintf("update imports in %s", baseName)
		}

		// Size-based heuristics
		if diff.LinesAdded > diff.LinesRemoved*3 {
			// Significant additions
			if commitType == "feat" {
				return fmt.Sprintf("extend %s functionality", baseName)
			}
		} else if diff.LinesRemoved > diff.LinesAdded*2 {
			// Significant removals
			if commitType == "refactor" {
				return fmt.Sprintf("simplify %s", baseName)
			}
			return fmt.Sprintf("clean up %s", baseName)
		}

		// Generic messages with context
		switch commitType {
		case "fix":
			return fmt.Sprintf("fix issues in %s", baseName)
		case "refactor":
			return fmt.Sprintf("refactor %s", baseName)
		case "feat":
			if dirName != "." && dirName != file {
				return fmt.Sprintf("enhance %s in %s", baseName, dirName)
			}
			return fmt.Sprintf("enhance %s", baseName)
		default:
			return fmt.Sprintf("update %s", baseName)
		}

	case "R":
		// Renamed files
		return fmt.Sprintf("rename %s", fileName)

	default:
		return fmt.Sprintf("modify %s", fileName)
	}
}

func determineScope(file string) string {
	// Enhanced scope detection
	parts := strings.Split(file, "/")

	// Check for common project structures
	if len(parts) > 1 {
		firstDir := parts[0]

		// Common directory patterns
		switch firstDir {
		case "src", "lib":
			if len(parts) > 2 {
				return parts[1]
			}
			return "core"
		case "tests", "test":
			return "test"
		case "docs", "documentation":
			return "docs"
		case "config", "configs":
			return "config"
		case "api":
			return "api"
		case "ui", "frontend", "client":
			return "ui"
		case "backend", "server":
			return "api"
		case "scripts", "tools":
			return "tools"
		default:
			return firstDir
		}
	}

	// File-based scope detection
	fileName := filepath.Base(file)
	if strings.Contains(fileName, "test") {
		return "test"
	}
	if strings.HasSuffix(fileName, ".md") {
		return "docs"
	}
	if strings.Contains(fileName, "config") {
		return "config"
	}

	return ""
}

func getStatusIcon(status string) string {
	if len(status) < 2 {
		return "📄 Unknown"
	}

	staged := status[0]
	unstaged := status[1]

	// Determine the base action
	var action string
	var icon string

	// Check the most significant change (staged takes priority, then unstaged)
	char := staged
	if char == ' ' || char == '?' {
		char = unstaged
	}

	switch char {
	case 'A':
		icon = "➕"
		action = "Added"
	case 'M':
		icon = "📝"
		action = "Modified"
	case 'D':
		icon = "🗑️"
		action = "Deleted"
	case 'R':
		icon = "📛"
		action = "Renamed"
	case 'C':
		icon = "📋"
		action = "Copied"
	case '?':
		icon = "❓"
		action = "Untracked"
	default:
		icon = "📄"
		action = "Changed"
	}

	// Determine staging status
	var stagingStatus string
	if staged != ' ' && staged != '?' && unstaged != ' ' && unstaged != '?' {
		stagingStatus = "Both"
	} else if staged != ' ' && staged != '?' {
		stagingStatus = "Staged"
	} else {
		stagingStatus = "Unstaged"
	}

	return fmt.Sprintf("%s %s (%s)", icon, action, stagingStatus)
}

// Commit convention and hook management
func (m model) generateCommitHook() tea.Cmd {
	return func() tea.Msg {
		hookPath := filepath.Join(m.repoPath, ".git", "hooks", "commit-msg")

		// Check if hook already exists
		if _, err := os.Stat(hookPath); err == nil {
			return statusMsg{message: "ℹ️ Commit hook already exists. Use 'H' (shift+h) to remove it."}
		}

		hookContent := `#!/bin/bash
# Git commit message hook generated by git-helper
# Enforces conventional commit format: type(scope): description
# 
# Valid types: feat, fix, docs, style, refactor, test, chore
# Example: feat(auth): add user authentication
# 
# This hook can be removed by deleting this file or using git-helper

commit_regex='^(feat|fix|docs|style|refactor|test|chore)(\(.+\))?: .{1,50}'

error_msg="❌ Invalid commit message format!

Expected format: <type>(<scope>): <description>

Valid types:
  • feat:     A new feature
  • fix:      A bug fix  
  • docs:     Documentation changes
  • style:    Code style changes (formatting, etc)
  • refactor: Code refactoring
  • test:     Adding or modifying tests
  • chore:    Build process or auxiliary tool changes

Examples:
  • feat(auth): add user authentication
  • fix(api): resolve timeout issue
  • docs: update README installation steps
  • test(utils): add validation tests

Your commit message:
$(cat $1)

To disable this check, delete: .git/hooks/commit-msg
Or use the git-helper tool (H key to remove)"

if ! grep -qE "$commit_regex" "$1"; then
    echo "$error_msg" >&2
    exit 1
fi
`

		err := os.WriteFile(hookPath, []byte(hookContent), 0755)
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to create commit hook: %v", err)}
		}

		return statusMsg{message: "✅ Commit hook installed at .git/hooks/commit-msg - validates conventional commit format"}
	}
}

func (m model) removeCommitHook() tea.Cmd {
	return func() tea.Msg {
		hookPath := filepath.Join(m.repoPath, ".git", "hooks", "commit-msg")

		// Check if hook exists
		if _, err := os.Stat(hookPath); os.IsNotExist(err) {
			return statusMsg{message: "ℹ️ No commit hook found to remove"}
		}

		err := os.Remove(hookPath)
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to remove commit hook: %v", err)}
		}

		return statusMsg{message: "✅ Commit hook removed - conventional commit validation disabled"}
	}
}

func (m model) checkHookStatus() tea.Cmd {
	return func() tea.Msg {
		hookPath := filepath.Join(m.repoPath, ".git", "hooks", "commit-msg")

		if _, err := os.Stat(hookPath); os.IsNotExist(err) {
			return statusMsg{message: "📋 Hook status: Not installed. Press 'h' to install conventional commit validation."}
		}

		return statusMsg{message: "📋 Hook status: Installed. Press 'H' (shift+h) to remove conventional commit validation."}
	}
}

func (m model) validateCommitMessage(message string) bool {
	// Basic validation for conventional commits
	conventionalRegex := `^(feat|fix|docs|style|refactor|test|chore)(\(.+\))?: .{1,50}`
	matched, _ := regexp.MatchString(conventionalRegex, message)
	return matched
}

func formatConventionalCommit(commitType, scope, description string) string {
	if scope != "" {
		return fmt.Sprintf("%s(%s): %s", commitType, scope, description)
	}
	return fmt.Sprintf("%s: %s", commitType, description)
}

func (m *model) updateFilesTable() {
	var rows []table.Row
	for _, change := range m.changes {
		// Analyze the change to get type and scope
		diffInfo := getFileDiff(change.File)
		analysis := analyzeFileChange(change, diffInfo)

		// Update the change with analysis results
		change.Type = analysis.Type
		change.Scope = analysis.Scope

		row := table.Row{
			getStatusIcon(change.Status),
			change.File,
			change.Type,
			change.Scope,
		}
		rows = append(rows, row)
	}
	m.filesTable.SetRows(rows)
}

func (m *model) updateSuggestionsTable() {
	var rows []table.Row
	for _, suggestion := range m.suggestions {
		row := table.Row{
			suggestion.Type,
			suggestion.Message,
		}
		rows = append(rows, row)
	}
	m.suggestionsTable.SetRows(rows)
}

func (m *model) adjustTableLayout() {
	availableWidth := m.width - 6

	// Adjust files table columns
	filesColumns := []table.Column{
		{Title: "Status", Width: 20},
		{Title: "File", Width: availableWidth - 60},
		{Title: "Type", Width: 20},
		{Title: "Scope", Width: 20},
	}
	m.filesTable.SetColumns(filesColumns)

	// Adjust suggestions table columns
	suggestionsColumns := []table.Column{
		{Title: "Type", Width: 12},
		{Title: "Message", Width: availableWidth - 15},
	}
	m.suggestionsTable.SetColumns(suggestionsColumns)

	// Adjust branches table columns
	branchesColumns := []table.Column{
		{Title: "Current", Width: 8},
		{Title: "Branch Name", Width: availableWidth - 50},
		{Title: "Upstream", Width: 35},
	}
	m.branchesTable.SetColumns(branchesColumns)

	// Adjust history table columns
	historyColumns := []table.Column{
		{Title: "Hash", Width: 10},
		{Title: "Message", Width: availableWidth - 50},
		{Title: "Author", Width: 20},
		{Title: "Date", Width: 15},
	}
	m.historyTable.SetColumns(historyColumns)
}

// Branch management functions
func (m model) loadBranches() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "branch", "-vv")
		cmd.Dir = m.repoPath
		output, err := cmd.Output()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to load branches: %v", err)}
		}

		var branches []Branch
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}

			isCurrent := strings.HasPrefix(line, "*")
			line = strings.TrimPrefix(line, "*")
			line = strings.TrimSpace(line)

			parts := strings.Fields(line)
			if len(parts) == 0 {
				continue
			}

			branch := Branch{
				Name:      parts[0],
				IsCurrent: isCurrent,
			}

			// Extract upstream info if present (appears in brackets)
			if strings.Contains(line, "[") && strings.Contains(line, "]") {
				start := strings.Index(line, "[")
				end := strings.Index(line, "]")
				if end > start {
					branch.Upstream = line[start+1 : end]
				}
			}

			branches = append(branches, branch)
		}

		return branchesMsg(branches)
	}
}

func (m model) switchBranch(branchName string) tea.Cmd {
	return func() tea.Msg {
		output, err := executeGitCommand(m.repoPath, "checkout", branchName)
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to switch branch: %v - %s", err, string(output))}
		}

		// Reload branches and git status
		return tea.Batch(
			m.loadBranches(),
			m.loadGitStatus(),
			func() tea.Msg {
				return statusMsg{message: fmt.Sprintf("✅ Switched to branch '%s'", branchName)}
			},
		)()
	}
}

func (m model) createBranch(branchName string) tea.Cmd {
	return func() tea.Msg {
		output, err := executeGitCommand(m.repoPath, "checkout", "-b", branchName)
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to create branch: %v - %s", err, string(output))}
		}

		// Reload branches and git status
		return tea.Batch(
			m.loadBranches(),
			m.loadGitStatus(),
			func() tea.Msg {
				return statusMsg{message: fmt.Sprintf("✅ Created and switched to branch '%s'", branchName)}
			},
		)()
	}
}

func (m model) deleteBranch(branchName string) tea.Cmd {
	return func() tea.Msg {
		output, err := executeGitCommand(m.repoPath, "branch", "-d", branchName)
		if err != nil {
			// Try force delete if regular delete fails
			output, err = executeGitCommand(m.repoPath, "branch", "-D", branchName)
			if err != nil {
				return statusMsg{message: fmt.Sprintf("❌ Failed to delete branch: %v - %s", err, string(output))}
			}
			return tea.Batch(
				m.loadBranches(),
				func() tea.Msg {
					return statusMsg{message: fmt.Sprintf("✅ Force deleted branch '%s'", branchName)}
				},
			)()
		}

		return tea.Batch(
			m.loadBranches(),
			func() tea.Msg {
				return statusMsg{message: fmt.Sprintf("✅ Deleted branch '%s'", branchName)}
			},
		)()
	}
}

func (m *model) updateBranchesTable() {
	var rows []table.Row
	for _, branch := range m.branches {
		current := ""
		if branch.IsCurrent {
			current = "→"
		}
		row := table.Row{
			current,
			branch.Name,
			branch.Upstream,
		}
		rows = append(rows, row)
	}
	m.branchesTable.SetRows(rows)
}

// Commit history functions
func (m model) loadHistory() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "log", "--pretty=format:%h|%s|%an|%ar", "-20")
		cmd.Dir = m.repoPath
		output, err := cmd.Output()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to load history: %v", err)}
		}

		var commits []Commit
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}

			parts := strings.Split(line, "|")
			if len(parts) >= 4 {
				commit := Commit{
					Hash:    parts[0],
					Message: parts[1],
					Author:  parts[2],
					Date:    parts[3],
				}
				commits = append(commits, commit)
			}
		}

		return commitsMsg(commits)
	}
}

func (m *model) updateHistoryTable() {
	var rows []table.Row
	for _, commit := range m.commits {
		row := table.Row{
			commit.Hash,
			commit.Message,
			commit.Author,
			commit.Date,
		}
		rows = append(rows, row)
	}
	m.historyTable.SetRows(rows)
}

// File staging functions
func (m model) toggleStaging(filePath string) tea.Cmd {
	return func() tea.Msg {
		// Check if file is staged
		statusCmd := exec.Command("git", "diff", "--cached", "--name-only")
		statusCmd.Dir = m.repoPath
		statusOutput, err := statusCmd.Output()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to check file status: %v", err)}
		}

		stagedFiles := strings.Split(strings.TrimSpace(string(statusOutput)), "\n")
		isStaged := false
		for _, f := range stagedFiles {
			if strings.TrimSpace(f) == filePath {
				isStaged = true
				break
			}
		}

		var gitCmd []string
		var action string
		if isStaged {
			gitCmd = []string{"reset", "HEAD", filePath}
			action = "unstaged"
		} else {
			gitCmd = []string{"add", filePath}
			action = "staged"
		}

		output, err := executeGitCommand(m.repoPath, gitCmd...)
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to %s file: %v - %s", action, err, string(output))}
		}

		// Reload changes and git status
		return tea.Batch(
			m.loadGitChanges(),
			m.loadGitStatus(),
			func() tea.Msg {
				return statusMsg{message: fmt.Sprintf("✅ %s: %s", strings.Title(action), filePath)}
			},
		)()
	}
}

// Diff viewing functions
func (m model) viewDiff(filePath string) tea.Cmd {
	return func() tea.Msg {
		// Try staged diff first
		cmd := exec.Command("git", "diff", "--cached", filePath)
		cmd.Dir = m.repoPath
		output, err := cmd.Output()

		// If no staged diff, try unstaged
		if err != nil || len(output) == 0 {
			cmd = exec.Command("git", "diff", filePath)
			cmd.Dir = m.repoPath
			output, err = cmd.Output()
		}

		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to get diff: %v", err)}
		}

		if len(output) == 0 {
			return statusMsg{message: "ℹ️ No changes to display"}
		}

		return diffMsg(string(output))
	}
}

// Pull functionality
func (m model) gitPull() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "pull")
		cmd.Dir = m.repoPath
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}

		output, err := cmd.CombinedOutput()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Git pull failed: %v - %s", err, string(output))}
		}

		// Reload everything after pull
		return tea.Batch(
			m.loadGitChanges(),
			m.loadGitStatus(),
			func() tea.Msg {
				return statusMsg{message: fmt.Sprintf("✅ Pull successful: %s", strings.TrimSpace(string(output)))}
			},
		)()
	}
}
