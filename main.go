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

// ============================================================================
// DATA STRUCTURES
// ============================================================================

type GitChange struct {
	File   string
	Status string
	Type   string
	Scope  string
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
	Ahead     int
	Behind    int
}

type Commit struct {
	Hash    string
	Message string
	Author  string
	Date    string
}

type DiffInfo struct {
	LinesAdded   int
	LinesRemoved int
	Functions    []string
	Imports      []string
	HasTests     bool
	HasDocs      bool
	Variables    []string
	Keywords     []string
	Comments     []string
	Context      string
}

type ConflictFile struct {
	Path       string
	Conflicts  []Conflict
	IsResolved bool
}

type Conflict struct {
	LineStart    int
	OursContent  []string
	TheirsContent []string
}

type BranchComparison struct {
	SourceBranch   string
	TargetBranch   string
	AheadCommits   []Commit
	BehindCommits  []Commit
	DifferingFiles []string
}

type RebaseCommit struct {
	Hash    string
	Message string
	Action  string // pick, squash, reword, edit, drop
}

// ============================================================================
// MODEL
// ============================================================================

type model struct {
	// State management - cleaner hierarchy
	tab       string // "workspace", "commit", "branches", "tools"
	toolMode  string // when tab="tools": "menu", "undo", "rebase", "history", "remote"
	viewMode  string // workspace sub-states: "files", "diff", "conflicts"

	// Data
	changes           []GitChange
	suggestions       []CommitSuggestion
	gitState          GitStatus
	branches          []Branch
	commits           []Commit
	conflicts         []ConflictFile
	branchComparison  *BranchComparison
	rebaseCommits     []RebaseCommit

	// UI content
	diffContent       string
	pushOutput        string
	recentCommits     []Commit // Last 3 for commit tab

	// Tables
	filesTable        table.Model
	branchesTable     table.Model
	toolsTable        table.Model
	historyTable      table.Model
	conflictsTable    table.Model
	comparisonTable   table.Model
	rebaseTable       table.Model
	undoTable         table.Model

	// Inputs
	commitInput       textinput.Model
	branchInput       textinput.Model
	rebaseInput       textinput.Model

	// UI state
	width             int
	height            int
	statusMsg         string
	statusExpiry      time.Time
	showDiffPreview   bool
	selectedSuggestion int // 0 = custom, 1-9 = suggestions

	// System
	repoPath          string
	lastCommit        string
	lastStatusUpdate  time.Time
	confirmAction     string
}

// ============================================================================
// MESSAGES
// ============================================================================

type statusMsg struct{ message string }
type gitChangesMsg []GitChange
type commitSuggestionsMsg []CommitSuggestion
type gitStatusMsg GitStatus
type branchesMsg []Branch
type commitsMsg []Commit
type recentCommitsMsg []Commit
type diffMsg string
type conflictsMsg []ConflictFile
type comparisonMsg BranchComparison
type rebaseCommitsMsg []RebaseCommit
type pushOutputMsg struct {
	output string
	commit string
}

// ============================================================================
// STYLES
// ============================================================================

var (
	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		MarginLeft(2)

	tabStyle = lipgloss.NewStyle().
		Padding(0, 2).
		Foreground(lipgloss.Color("240"))

	activeTabStyle = lipgloss.NewStyle().
		Padding(0, 2).
		Foreground(lipgloss.Color("86")).
		Bold(true).
		Underline(true)

	suggestionStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		MarginLeft(2)

	selectedSuggestionStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("46")).
		Bold(true).
		MarginLeft(2)

	helpStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		MarginLeft(2)

	errorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).
		Bold(true)

	successStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("46")).
		Bold(true)

	warningStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("226")).
		Bold(true)
)

// ============================================================================
// INITIALIZATION
// ============================================================================

func initialModel() model {
	// Get repository path
	repoPath, err := os.Getwd()
	if err != nil {
		repoPath = "."
	}

	// Initialize commit input (always visible in commit tab)
	commitInput := textinput.New()
	commitInput.Placeholder = "Or type your custom commit message..."
	commitInput.CharLimit = 200

	// Initialize branch input
	branchInput := textinput.New()
	branchInput.Placeholder = "Branch name..."
	branchInput.CharLimit = 100

	// Initialize rebase input
	rebaseInput := textinput.New()
	rebaseInput.Placeholder = "Number of commits to rebase..."
	rebaseInput.CharLimit = 3

	m := model{
		tab:                "workspace",
		toolMode:           "menu",
		viewMode:           "files",
		repoPath:           repoPath,
		commitInput:        commitInput,
		branchInput:        branchInput,
		rebaseInput:        rebaseInput,
		showDiffPreview:    true,
		selectedSuggestion: 0,
	}

	// Initialize tables (will be populated later)
	m.filesTable = createFilesTable()
	m.branchesTable = createBranchesTable()
	m.toolsTable = createToolsTable()
	m.historyTable = createHistoryTable()
	m.conflictsTable = createConflictsTable()
	m.comparisonTable = createComparisonTable()
	m.rebaseTable = createRebaseTable()
	m.undoTable = createUndoTable()

	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.loadGitChanges(),
		m.loadGitStatus(),
		m.loadRecentCommits(),
	)
}

// ============================================================================
// TABLE CREATION
// ============================================================================

func createFilesTable() table.Model {
	columns := []table.Column{
		{Title: "Status", Width: 10},
		{Title: "File", Width: 50},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)

	t.SetStyles(s)
	return t
}

func createBranchesTable() table.Model {
	columns := []table.Column{
		{Title: "Branch", Width: 30},
		{Title: "Status", Width: 20},
		{Title: "Upstream", Width: 30},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)

	t.SetStyles(s)
	return t
}

func createToolsTable() table.Model {
	columns := []table.Column{
		{Title: "Tool", Width: 30},
		{Title: "Description", Width: 50},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(6),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)

	t.SetStyles(s)
	return t
}

func createHistoryTable() table.Model {
	columns := []table.Column{
		{Title: "Hash", Width: 10},
		{Title: "Message", Width: 50},
		{Title: "Author", Width: 20},
		{Title: "Date", Width: 20},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(15),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)

	t.SetStyles(s)
	return t
}

func createConflictsTable() table.Model {
	columns := []table.Column{
		{Title: "File", Width: 50},
		{Title: "Conflicts", Width: 15},
		{Title: "Status", Width: 15},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)

	t.SetStyles(s)
	return t
}

func createComparisonTable() table.Model {
	columns := []table.Column{
		{Title: "Type", Width: 15},
		{Title: "Hash", Width: 10},
		{Title: "Message", Width: 55},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(15),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)

	t.SetStyles(s)
	return t
}

func createRebaseTable() table.Model {
	columns := []table.Column{
		{Title: "Action", Width: 10},
		{Title: "Hash", Width: 10},
		{Title: "Message", Width: 60},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(15),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)

	t.SetStyles(s)
	return t
}

func createUndoTable() table.Model {
	columns := []table.Column{
		{Title: "Option", Width: 20},
		{Title: "Description", Width: 60},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(6),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)

	t.SetStyles(s)

	// Set the undo options rows
	rows := []table.Row{
		{"Soft Reset", "Undo commit, keep changes staged"},
		{"Mixed Reset", "Undo commit, unstage changes"},
		{"Hard Reset", "Undo commit, DISCARD all changes (DANGEROUS!)"},
		{"View Reflog", "See all recent actions"},
	}
	t.SetRows(rows)

	return t
}

// ============================================================================
// UPDATE
// ============================================================================

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.adjustTableSizes()
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case gitChangesMsg:
		m.changes = []GitChange(msg)
		m.updateFilesTable()
		// Auto-detect conflicts
		if m.hasConflicts() {
			return m, m.loadConflicts()
		}
		return m, m.generateCommitSuggestions()

	case commitSuggestionsMsg:
		m.suggestions = []CommitSuggestion(msg)
		return m, nil

	case gitStatusMsg:
		m.gitState = GitStatus(msg)
		m.lastStatusUpdate = time.Now()
		return m, nil

	case branchesMsg:
		m.branches = []Branch(msg)
		m.updateBranchesTable()
		return m, nil

	case commitsMsg:
		m.commits = []Commit(msg)
		m.updateHistoryTable()
		return m, nil

	case recentCommitsMsg:
		m.recentCommits = []Commit(msg)
		return m, nil

	case diffMsg:
		m.diffContent = string(msg)
		return m, nil

	case conflictsMsg:
		m.conflicts = []ConflictFile(msg)
		m.updateConflictsTable()
		// Auto-switch to conflicts view
		if len(m.conflicts) > 0 {
			m.viewMode = "conflicts"
		}
		return m, nil

	case comparisonMsg:
		comp := BranchComparison(msg)
		m.branchComparison = &comp
		m.updateComparisonTable()
		return m, nil

	case rebaseCommitsMsg:
		m.rebaseCommits = []RebaseCommit(msg)
		m.updateRebaseTable()
		return m, nil

	case statusMsg:
		m.statusMsg = msg.message
		m.statusExpiry = time.Now().Add(3 * time.Second)
		return m, nil

	case pushOutputMsg:
		m.pushOutput = msg.output
		m.lastCommit = msg.commit
		m.statusMsg = "✅ Pushed successfully"
		m.statusExpiry = time.Now().Add(3 * time.Second)
		return m, nil
	}

	// Update appropriate component based on state
	switch m.tab {
	case "workspace":
		if m.viewMode == "files" {
			m.filesTable, cmd = m.filesTable.Update(msg)
			cmds = append(cmds, cmd)
		} else if m.viewMode == "conflicts" {
			m.conflictsTable, cmd = m.conflictsTable.Update(msg)
			cmds = append(cmds, cmd)
		}
	case "commit":
		if m.commitInput.Focused() {
			m.commitInput, cmd = m.commitInput.Update(msg)
			cmds = append(cmds, cmd)
		}
	case "branches":
		if m.branchInput.Focused() {
			m.branchInput, cmd = m.branchInput.Update(msg)
			cmds = append(cmds, cmd)
		} else if m.branchComparison != nil {
			m.comparisonTable, cmd = m.comparisonTable.Update(msg)
			cmds = append(cmds, cmd)
		} else {
			m.branchesTable, cmd = m.branchesTable.Update(msg)
			cmds = append(cmds, cmd)
		}
	case "tools":
		switch m.toolMode {
		case "menu":
			m.toolsTable, cmd = m.toolsTable.Update(msg)
			cmds = append(cmds, cmd)
		case "undo":
			m.undoTable, cmd = m.undoTable.Update(msg)
			cmds = append(cmds, cmd)
		case "history":
			m.historyTable, cmd = m.historyTable.Update(msg)
			cmds = append(cmds, cmd)
		case "rebase":
			if m.rebaseInput.Focused() {
				m.rebaseInput, cmd = m.rebaseInput.Update(msg)
				cmds = append(cmds, cmd)
			} else {
				m.rebaseTable, cmd = m.rebaseTable.Update(msg)
				cmds = append(cmds, cmd)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

// ============================================================================
// KEY HANDLING
// ============================================================================

func (m model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global escape handling
	if msg.String() == "esc" {
		return m.handleEscape()
	}

	// Global quit
	if msg.String() == "ctrl+c" || (msg.String() == "q" && !m.anyInputFocused()) {
		return m, tea.Quit
	}

	// Tab switching (1-4) - only when no input is focused
	if !m.anyInputFocused() {
		switch msg.String() {
		case "1":
			m.tab = "workspace"
			m.viewMode = "files"
			return m, nil
		case "2":
			if m.gitState.StagedFiles > 0 {
				m.tab = "commit"
				m.selectedSuggestion = 0
			} else {
				m.statusMsg = "❌ No files staged. Stage files first in workspace."
				m.statusExpiry = time.Now().Add(3 * time.Second)
			}
			return m, nil
		case "3":
			m.tab = "branches"
			m.branchComparison = nil // Clear comparison when entering tab
			return m, m.loadBranches()
		case "4":
			m.tab = "tools"
			m.toolMode = "menu"
			m.updateToolsTable()
			return m, nil
		}
	}

	// Handle based on current tab
	switch m.tab {
	case "workspace":
		return m.handleWorkspaceKeys(msg)
	case "commit":
		return m.handleCommitKeys(msg)
	case "branches":
		return m.handleBranchesKeys(msg)
	case "tools":
		return m.handleToolsKeys(msg)
	}

	return m, nil
}

func (m model) handleEscape() (tea.Model, tea.Cmd) {
	// Blur any focused inputs
	if m.commitInput.Focused() {
		m.commitInput.Blur()
		m.selectedSuggestion = 0
		return m, nil
	}
	if m.branchInput.Focused() {
		m.branchInput.Blur()
		m.branchInput.SetValue("")
		return m, nil
	}
	if m.rebaseInput.Focused() {
		m.rebaseInput.Blur()
		m.rebaseInput.SetValue("")
		return m, nil
	}

	// Exit sub-modes
	if m.viewMode == "diff" {
		m.viewMode = "files"
		return m, nil
	}
	if m.branchComparison != nil {
		m.branchComparison = nil
		return m, nil
	}
	if m.tab == "tools" && m.toolMode != "menu" {
		m.toolMode = "menu"
		m.updateToolsTable()
		return m, nil
	}

	// Clear confirmation
	if m.confirmAction != "" {
		m.confirmAction = ""
		m.statusMsg = "❌ Action cancelled"
		m.statusExpiry = time.Now().Add(2 * time.Second)
		return m, nil
	}

	return m, nil
}

func (m model) anyInputFocused() bool {
	return m.commitInput.Focused() || m.branchInput.Focused() || m.rebaseInput.Focused()
}

// ============================================================================
// WORKSPACE TAB KEY HANDLING
// ============================================================================

func (m model) handleWorkspaceKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.viewMode == "conflicts" {
		return m.handleConflictKeys(msg)
	}

	switch msg.String() {
	case " ": // Space - toggle staging
		if len(m.changes) > 0 {
			selectedIndex := m.filesTable.Cursor()
			if selectedIndex < len(m.changes) {
				return m, m.toggleStaging(m.changes[selectedIndex].File)
			}
		}
		return m, nil

	case "a": // Stage all
		return m, tea.Batch(m.gitAddAll(), m.refreshAfterStaging())

	case "r": // Refresh
		return m, tea.Batch(m.loadGitChanges(), m.loadGitStatus())

	case "v": // Toggle diff preview
		m.showDiffPreview = !m.showDiffPreview
		if m.showDiffPreview && len(m.changes) > 0 {
			selectedIndex := m.filesTable.Cursor()
			if selectedIndex < len(m.changes) {
				return m, m.viewDiff(m.changes[selectedIndex].File)
			}
		}
		return m, nil

	case "d": // View full diff
		if len(m.changes) > 0 {
			selectedIndex := m.filesTable.Cursor()
			if selectedIndex < len(m.changes) {
				m.viewMode = "diff"
				return m, m.viewDiff(m.changes[selectedIndex].File)
			}
		}
		return m, nil

	case "R": // Reset (unstage all)
		return m, tea.Batch(m.gitReset(), m.refreshAfterStaging())
	}

	// DEBUG: Log what key we're passing to table
	if msg.String() == "up" || msg.String() == "down" {
		m.statusMsg = fmt.Sprintf("DEBUG: Passing %s to filesTable", msg.String())
		m.statusExpiry = time.Now().Add(1 * time.Second)
	}

	// Pass unhandled keys to the table for navigation
	var cmd tea.Cmd
	m.filesTable, cmd = m.filesTable.Update(msg)
	return m, cmd
}

func (m model) handleConflictKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.conflicts) == 0 {
		return m, nil
	}

	selectedIndex := m.conflictsTable.Cursor()
	if selectedIndex >= len(m.conflicts) {
		return m, nil
	}

	switch msg.String() {
	case "o": // Accept ours
		return m, m.resolveConflict(selectedIndex, "ours")
	case "t": // Accept theirs
		return m, m.resolveConflict(selectedIndex, "theirs")
	case "b": // Accept both
		return m, m.resolveConflict(selectedIndex, "both")
	case "enter": // View conflict details
		// TODO: Show detailed conflict view
		return m, nil
	case "c": // Continue merge (if all resolved)
		if m.allConflictsResolved() {
			return m, m.continueMerge()
		} else {
			m.statusMsg = "❌ Resolve all conflicts before continuing"
			m.statusExpiry = time.Now().Add(3 * time.Second)
			return m, nil
		}
	}

	// Pass unhandled keys to the conflicts table for navigation
	var cmd tea.Cmd
	m.conflictsTable, cmd = m.conflictsTable.Update(msg)
	return m, cmd
}

// ============================================================================
// COMMIT TAB KEY HANDLING
// ============================================================================

func (m model) handleCommitKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If input is focused, handle text input
	if m.commitInput.Focused() {
		if msg.String() == "enter" {
			customMsg := m.commitInput.Value()
			if customMsg != "" {
				m.commitInput.SetValue("")
				m.commitInput.Blur()
				m.selectedSuggestion = 0
				return m, tea.Batch(
					m.commitWithMessage(customMsg),
					m.refreshAfterCommit(),
				)
			}
		}
		return m, nil
	}

	// Arrow keys to select suggestion
	if msg.String() == "up" || msg.String() == "k" {
		if m.selectedSuggestion > 0 {
			m.selectedSuggestion--
		}
		return m, nil
	}
	if msg.String() == "down" || msg.String() == "j" {
		if m.selectedSuggestion < len(m.suggestions) {
			m.selectedSuggestion++
		}
		return m, nil
	}

	// Enter on selected suggestion - CHECK THIS FIRST before focusing input
	if msg.String() == "enter" && m.selectedSuggestion > 0 {
		if m.selectedSuggestion <= len(m.suggestions) {
			suggestion := m.suggestions[m.selectedSuggestion-1]
			return m, tea.Batch(
				m.commitWithMessage(suggestion.Message),
				m.refreshAfterCommit(),
			)
		}
		return m, nil
	}

	// Spacebar on selected suggestion
	if msg.String() == " " && m.selectedSuggestion > 0 {
		if m.selectedSuggestion <= len(m.suggestions) {
			suggestion := m.suggestions[m.selectedSuggestion-1]
			return m, tea.Batch(
				m.commitWithMessage(suggestion.Message),
				m.refreshAfterCommit(),
			)
		}
		return m, nil
	}

	// 'c' or Enter (when no suggestion selected) to focus custom input
	if msg.String() == "c" || (msg.String() == "enter" && m.selectedSuggestion == 0) {
		m.commitInput.Focus()
		m.selectedSuggestion = -1
		return m, nil
	}

	return m, nil
}

// ============================================================================
// BRANCHES TAB KEY HANDLING
// ============================================================================

func (m model) handleBranchesKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle branch input
	if m.branchInput.Focused() {
		if msg.String() == "enter" {
			branchName := m.branchInput.Value()
			if branchName != "" {
				m.branchInput.SetValue("")
				m.branchInput.Blur()
				return m, m.createBranch(branchName)
			}
		}
		return m, nil
	}

	// If in comparison mode, pass keys to comparison table for navigation
	if m.branchComparison != nil {
		var cmd tea.Cmd
		m.comparisonTable, cmd = m.comparisonTable.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "enter": // Switch to selected branch
		if len(m.branches) > 0 {
			selectedIndex := m.branchesTable.Cursor()
			if selectedIndex < len(m.branches) {
				branch := m.branches[selectedIndex]
				if !branch.IsCurrent {
					return m, m.switchBranch(branch.Name)
				}
			}
		}
		return m, nil

	case "n": // New branch
		m.branchInput.Focus()
		return m, nil

	case "d": // Delete branch
		if len(m.branches) > 0 {
			selectedIndex := m.branchesTable.Cursor()
			if selectedIndex < len(m.branches) {
				branch := m.branches[selectedIndex]
				if branch.IsCurrent {
					m.statusMsg = "❌ Cannot delete current branch"
					m.statusExpiry = time.Now().Add(3 * time.Second)
					return m, nil
				}
				m.confirmAction = "delete-branch:" + branch.Name
				m.statusMsg = fmt.Sprintf("⚠️ Press 'y' to confirm delete '%s', or ESC to cancel", branch.Name)
				m.statusExpiry = time.Now().Add(10 * time.Second)
			}
		}
		return m, nil

	case "y": // Confirm delete
		if strings.HasPrefix(m.confirmAction, "delete-branch:") {
			branchName := strings.TrimPrefix(m.confirmAction, "delete-branch:")
			m.confirmAction = ""
			return m, m.deleteBranch(branchName)
		}
		return m, nil

	case "c": // Compare with another branch
		// Default compare with main/master
		targetBranch := "main"
		// Check if main exists, otherwise try master
		for _, b := range m.branches {
			if b.Name == "main" {
				targetBranch = "main"
				break
			} else if b.Name == "master" {
				targetBranch = "master"
			}
		}
		return m, m.loadBranchComparison(targetBranch)

	case "r": // Refresh branches
		return m, m.loadBranches()
	}

	// Pass unhandled keys to the branches table for navigation
	var cmd tea.Cmd
	m.branchesTable, cmd = m.branchesTable.Update(msg)
	return m, cmd
}

// ============================================================================
// TOOLS TAB KEY HANDLING
// ============================================================================

func (m model) handleToolsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.toolMode {
	case "menu":
		return m.handleToolsMenuKeys(msg)
	case "undo":
		return m.handleUndoKeys(msg)
	case "rebase":
		return m.handleRebaseKeys(msg)
	case "history":
		return m.handleHistoryKeys(msg)
	case "remote":
		return m.handleRemoteKeys(msg)
	}
	return m, nil
}

func (m model) handleToolsMenuKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Only use Enter to select (no number keys - they conflict with tab switching)
	if msg.String() == "enter" {
		switch m.toolsTable.Cursor() {
		case 0:
			m.toolMode = "undo"
			return m, nil
		case 1:
			m.toolMode = "rebase"
			m.rebaseInput.Focus()
			return m, nil
		case 2:
			m.toolMode = "history"
			return m, m.loadHistory()
		case 3:
			m.toolMode = "remote"
			return m, nil
		}
	}

	// Pass unhandled keys to the tools table for navigation
	var cmd tea.Cmd
	m.toolsTable, cmd = m.toolsTable.Update(msg)
	return m, cmd
}

func (m model) handleUndoKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle enter key based on cursor position
	if msg.String() == "enter" {
		switch m.undoTable.Cursor() {
		case 0: // Soft reset
			m.confirmAction = "soft-reset"
			m.statusMsg = "⚠️ Press 'y' to undo last commit (keep changes), or ESC to cancel"
			m.statusExpiry = time.Now().Add(10 * time.Second)
			return m, nil
		case 1: // Mixed reset
			m.confirmAction = "mixed-reset"
			m.statusMsg = "⚠️ Press 'y' to undo last commit (unstage changes), or ESC to cancel"
			m.statusExpiry = time.Now().Add(10 * time.Second)
			return m, nil
		case 2: // Hard reset (DANGEROUS)
			m.confirmAction = "hard-reset"
			m.statusMsg = "⚠️⚠️⚠️ DANGEROUS: Press 'y' to undo and DISCARD changes, or ESC to cancel"
			m.statusExpiry = time.Now().Add(15 * time.Second)
			return m, nil
		case 3: // View reflog
			return m, m.loadReflog()
		}
	}

	// Handle confirmation
	if msg.String() == "y" {
		switch m.confirmAction {
		case "soft-reset":
			m.confirmAction = ""
			return m, m.softReset(1)
		case "mixed-reset":
			m.confirmAction = ""
			return m, m.mixedReset(1)
		case "hard-reset":
			m.confirmAction = ""
			return m, m.hardReset(1)
		}
		return m, nil
	}

	// Pass unhandled keys to the undo table for navigation
	var cmd tea.Cmd
	m.undoTable, cmd = m.undoTable.Update(msg)
	return m, cmd
}

func (m model) handleRebaseKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If input is focused, handle count input
	if m.rebaseInput.Focused() {
		if msg.String() == "enter" {
			countStr := m.rebaseInput.Value()
			if countStr != "" {
				count, err := strconv.Atoi(countStr)
				if err == nil && count > 0 && count <= 50 {
					m.rebaseInput.SetValue("")
					m.rebaseInput.Blur()
					return m, m.loadRebaseCommits(count)
				} else {
					m.statusMsg = "❌ Invalid count (must be 1-50)"
					m.statusExpiry = time.Now().Add(3 * time.Second)
				}
			}
		}
		return m, nil
	}

	// If commits are loaded, handle rebase actions
	if len(m.rebaseCommits) > 0 {
		selectedIndex := m.rebaseTable.Cursor()
		if selectedIndex >= len(m.rebaseCommits) {
			return m, nil
		}

		switch msg.String() {
		case "p":
			m.rebaseCommits[selectedIndex].Action = "pick"
			m.updateRebaseTable()
			return m, nil
		case "s":
			m.rebaseCommits[selectedIndex].Action = "squash"
			m.updateRebaseTable()
			return m, nil
		case "r":
			m.rebaseCommits[selectedIndex].Action = "reword"
			m.updateRebaseTable()
			return m, nil
		case "d":
			m.rebaseCommits[selectedIndex].Action = "drop"
			m.updateRebaseTable()
			return m, nil
		case "f":
			m.rebaseCommits[selectedIndex].Action = "fixup"
			m.updateRebaseTable()
			return m, nil
		case "enter":
			// Execute rebase
			m.confirmAction = "execute-rebase"
			m.statusMsg = "⚠️ Press 'y' to execute rebase, or ESC to cancel"
			m.statusExpiry = time.Now().Add(10 * time.Second)
			return m, nil
		case "y":
			if m.confirmAction == "execute-rebase" {
				m.confirmAction = ""
				return m, m.executeRebase()
			}
			return m, nil
		}

		// Pass unhandled keys to the rebase table for navigation
		var cmd tea.Cmd
		m.rebaseTable, cmd = m.rebaseTable.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m model) handleHistoryKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r":
		return m, m.loadHistory()
	case "c": // Copy hash
		if len(m.commits) > 0 {
			selectedIndex := m.historyTable.Cursor()
			if selectedIndex < len(m.commits) {
				// Would need clipboard support - for now just show message
				m.statusMsg = fmt.Sprintf("📋 Hash: %s", m.commits[selectedIndex].Hash)
				m.statusExpiry = time.Now().Add(5 * time.Second)
			}
		}
		return m, nil
	}

	// Pass unhandled keys to the history table for navigation
	var cmd tea.Cmd
	m.historyTable, cmd = m.historyTable.Update(msg)
	return m, cmd
}

func (m model) handleRemoteKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "p": // Push
		return m, m.gitPush()
	case "l": // Pull
		return m, m.gitPull()
	case "f": // Fetch
		return m, m.gitFetch()
	}
	return m, nil
}

// ============================================================================
// GIT OPERATIONS - Loading Data
// ============================================================================

func (m model) loadGitChanges() tea.Cmd {
	return func() tea.Msg {
		statusCmd := exec.Command("git", "status", "--porcelain")
		statusCmd.Dir = m.repoPath
		statusOutput, err := statusCmd.Output()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to get status: %v", err)}
		}

		changes := parseGitStatus(string(statusOutput))
		return gitChangesMsg(changes)
	}
}

func (m model) loadGitStatus() tea.Cmd {
	return func() tea.Msg {
		status := GitStatus{}
		status.Branch = getBranchName(m.repoPath)

		statusCmd := exec.Command("git", "status", "--porcelain")
		statusCmd.Dir = m.repoPath
		statusOutput, err := statusCmd.Output()
		if err == nil {
			statusText := string(statusOutput)
			status.StagedFiles, status.UnstagedFiles, status.Clean = parseGitStatusOutput(statusText)
		}

		status.Ahead, status.Behind = getAheadBehindCount(m.repoPath)
		return gitStatusMsg(status)
	}
}

func (m model) loadBranches() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "branch", "-vv")
		cmd.Dir = m.repoPath
		output, err := cmd.Output()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to load branches: %v", err)}
		}

		var branches []Branch
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			branch := Branch{}
			if strings.HasPrefix(line, "*") {
				branch.IsCurrent = true
				line = strings.TrimPrefix(line, "* ")
			} else {
				line = strings.TrimPrefix(line, "  ")
			}

			parts := strings.Fields(line)
			if len(parts) > 0 {
				branch.Name = parts[0]
			}
			if len(parts) > 2 {
				branch.Upstream = parts[2]
			}

			// Parse ahead/behind from upstream info
			if strings.Contains(line, "ahead") {
				re := regexp.MustCompile(`ahead (\d+)`)
				if matches := re.FindStringSubmatch(line); len(matches) > 1 {
					branch.Ahead, _ = strconv.Atoi(matches[1])
				}
			}
			if strings.Contains(line, "behind") {
				re := regexp.MustCompile(`behind (\d+)`)
				if matches := re.FindStringSubmatch(line); len(matches) > 1 {
					branch.Behind, _ = strconv.Atoi(matches[1])
				}
			}

			branches = append(branches, branch)
		}

		return branchesMsg(branches)
	}
}

func (m model) loadHistory() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "log", "-20", "--pretty=format:%h|%s|%an|%ar")
		cmd.Dir = m.repoPath
		output, err := cmd.Output()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to load history: %v", err)}
		}

		var commits []Commit
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			parts := strings.Split(line, "|")
			if len(parts) >= 4 {
				commits = append(commits, Commit{
					Hash:    parts[0],
					Message: parts[1],
					Author:  parts[2],
					Date:    parts[3],
				})
			}
		}

		return commitsMsg(commits)
	}
}

func (m model) loadRecentCommits() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "log", "-3", "--pretty=format:%h|%s|%an|%ar")
		cmd.Dir = m.repoPath
		output, err := cmd.Output()
		if err != nil {
			return recentCommitsMsg([]Commit{})
		}

		var commits []Commit
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			parts := strings.Split(line, "|")
			if len(parts) >= 4 {
				commits = append(commits, Commit{
					Hash:    parts[0],
					Message: parts[1],
					Author:  parts[2],
					Date:    parts[3],
				})
			}
		}

		return recentCommitsMsg(commits)
	}
}

func (m model) loadConflicts() tea.Cmd {
	return func() tea.Msg {
		// Get list of conflicted files
		cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
		cmd.Dir = m.repoPath
		output, err := cmd.Output()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to detect conflicts: %v", err)}
		}

		var conflictFiles []ConflictFile
		files := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, file := range files {
			if file == "" {
				continue
			}

			// Read file and parse conflicts
			content, err := os.ReadFile(filepath.Join(m.repoPath, file))
			if err != nil {
				continue
			}

			conflicts := parseConflictMarkers(string(content))
			if len(conflicts) > 0 {
				conflictFiles = append(conflictFiles, ConflictFile{
					Path:       file,
					Conflicts:  conflicts,
					IsResolved: false,
				})
			}
		}

		return conflictsMsg(conflictFiles)
	}
}

func (m model) loadBranchComparison(targetBranch string) tea.Cmd {
	return func() tea.Msg {
		comparison := BranchComparison{
			SourceBranch: getBranchName(m.repoPath),
			TargetBranch: targetBranch,
		}

		// Get commits ahead (on current branch but not on target)
		aheadCmd := exec.Command("git", "log", "--pretty=format:%h|%s|%an|%ar", targetBranch+"..HEAD")
		aheadCmd.Dir = m.repoPath
		aheadOutput, err := aheadCmd.Output()
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(aheadOutput)), "\n")
			for _, line := range lines {
				if line == "" {
					continue
				}
				parts := strings.Split(line, "|")
				if len(parts) >= 4 {
					comparison.AheadCommits = append(comparison.AheadCommits, Commit{
						Hash:    parts[0],
						Message: parts[1],
						Author:  parts[2],
						Date:    parts[3],
					})
				}
			}
		}

		// Get commits behind (on target branch but not on current)
		behindCmd := exec.Command("git", "log", "--pretty=format:%h|%s|%an|%ar", "HEAD.."+targetBranch)
		behindCmd.Dir = m.repoPath
		behindOutput, err := behindCmd.Output()
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(behindOutput)), "\n")
			for _, line := range lines {
				if line == "" {
					continue
				}
				parts := strings.Split(line, "|")
				if len(parts) >= 4 {
					comparison.BehindCommits = append(comparison.BehindCommits, Commit{
						Hash:    parts[0],
						Message: parts[1],
						Author:  parts[2],
						Date:    parts[3],
					})
				}
			}
		}

		// Get differing files
		diffCmd := exec.Command("git", "diff", "--name-only", targetBranch+"...HEAD")
		diffCmd.Dir = m.repoPath
		diffOutput, err := diffCmd.Output()
		if err == nil {
			files := strings.Split(strings.TrimSpace(string(diffOutput)), "\n")
			for _, file := range files {
				if file != "" {
					comparison.DifferingFiles = append(comparison.DifferingFiles, file)
				}
			}
		}

		return comparisonMsg(comparison)
	}
}

func (m model) loadRebaseCommits(count int) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "log", fmt.Sprintf("-%d", count), "--pretty=format:%h|%s")
		cmd.Dir = m.repoPath
		output, err := cmd.Output()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to load commits: %v", err)}
		}

		var commits []RebaseCommit
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		// Reverse order (oldest first for rebase)
		for i := len(lines) - 1; i >= 0; i-- {
			line := lines[i]
			if line == "" {
				continue
			}
			parts := strings.Split(line, "|")
			if len(parts) >= 2 {
				commits = append(commits, RebaseCommit{
					Hash:    parts[0],
					Message: parts[1],
					Action:  "pick", // Default action
				})
			}
		}

		return rebaseCommitsMsg(commits)
	}
}

func (m model) loadReflog() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "reflog", "-20", "--pretty=format:%h|%s|%ar")
		cmd.Dir = m.repoPath
		output, err := cmd.Output()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to load reflog: %v", err)}
		}

		var commits []Commit
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			parts := strings.Split(line, "|")
			if len(parts) >= 3 {
				commits = append(commits, Commit{
					Hash:    parts[0],
					Message: parts[1],
					Date:    parts[2],
				})
			}
		}

		return commitsMsg(commits)
	}
}

// (Continuing in next part due to length...)
// ============================================================================
// GIT OPERATIONS - Commit Suggestions
// ============================================================================

func (m model) generateCommitSuggestions() tea.Cmd {
	return func() tea.Msg {
		suggestions := analyzeChangesForCommits(m.changes, m.repoPath)
		return commitSuggestionsMsg(suggestions)
	}
}

func analyzeChangesForCommits(changes []GitChange, repoPath string) []CommitSuggestion {
	var suggestions []CommitSuggestion

	// Generate individual suggestions
	individualSuggestions := []CommitSuggestion{}
	for _, change := range changes {
		diffInfo := getFileDiff(change.File, repoPath)
		analysis := analyzeFileChange(change, diffInfo)

		suggestion := CommitSuggestion{
			Type:    analysis.Type,
			Message: analysis.Message,
		}
		individualSuggestions = append(individualSuggestions, suggestion)
	}

	// Create combined suggestion as first option
	combinedSuggestion := generateCombinedSuggestion(individualSuggestions, changes)
	if combinedSuggestion.Message != "" {
		suggestions = append(suggestions, combinedSuggestion)
	}

	// Add individual suggestions if there are 5 or fewer files
	if len(individualSuggestions) <= 5 {
		suggestions = append(suggestions, individualSuggestions...)
	}

	// Limit to max 9 suggestions total (to match 1-9 keys)
	if len(suggestions) > 9 {
		suggestions = suggestions[:9]
	}

	return suggestions
}

func generateCombinedSuggestion(individual []CommitSuggestion, changes []GitChange) CommitSuggestion {
	if len(individual) == 0 {
		return CommitSuggestion{}
	}

	// Count types to determine the main focus
	typeCounts := make(map[string]int)
	scopeCounts := make(map[string]int)
	functionNames := []string{}

	for i, suggestion := range individual {
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

		// Extract function names from messages (simplified)
		if i < len(changes) {
			parts := strings.Split(suggestion.Message, " ")
			if len(parts) > 2 {
				functionNames = append(functionNames, parts[len(parts)-1])
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

	// Generate a smart combined message
	var description string
	totalFiles := len(individual)

	if len(typeCounts) == 1 {
		// All changes are the same type - be more specific
		switch mainType {
		case "feat":
			if totalFiles == 1 {
				description = "add new feature"
			} else {
				description = fmt.Sprintf("add new features across %d files", totalFiles)
			}
		case "fix":
			if totalFiles == 1 {
				description = "fix bug"
			} else {
				description = fmt.Sprintf("fix multiple bugs (%d files)", totalFiles)
			}
		case "docs":
			description = "update documentation"
		case "test":
			description = "update tests"
		case "chore":
			description = "update configuration"
		case "refactor":
			description = "refactor code"
		default:
			description = "update files"
		}
	} else {
		// Mixed types - provide context
		description = fmt.Sprintf("update %d files with mixed changes", totalFiles)
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

func getFileDiff(filePath string, repoPath string) DiffInfo {
	cmd := exec.Command("git", "diff", "--cached", filePath)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		// Try unstaged diff if no staged changes
		cmd = exec.Command("git", "diff", filePath)
		cmd.Dir = repoPath
		output, _ = cmd.Output()
	}

	return parseDiffOutput(string(output))
}

func parseDiffOutput(diff string) DiffInfo {
	info := DiffInfo{}
	lines := strings.Split(diff, "\n")

	// Keywords to look for that indicate specific types of changes
	bugKeywords := []string{"bug", "fix", "error", "crash", "issue", "problem"}
	validationKeywords := []string{"validate", "validation", "check", "verify", "sanitize"}
	apiKeywords := []string{"endpoint", "route", "handler", "api", "request", "response"}
	securityKeywords := []string{"auth", "security", "permission", "token", "encrypt"}
	performanceKeywords := []string{"optimize", "performance", "cache", "speed", "efficient"}

	contextCounts := make(map[string]int)

	for _, line := range lines {
		lineLower := strings.ToLower(line)

		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			info.LinesAdded++

			// Detect function definitions
			funcName := extractFunctionName(line)
			if funcName != "" && !contains(info.Functions, funcName) {
				info.Functions = append(info.Functions, funcName)
			}

			// Detect variable names
			varName := extractVariableName(line)
			if varName != "" && !contains(info.Variables, varName) {
				info.Variables = append(info.Variables, varName)
			}

			// Detect imports/includes
			if isImportLine(line) {
				importName := extractImportName(line)
				if importName != "" && !contains(info.Imports, importName) {
					info.Imports = append(info.Imports, importName)
				}
			}

			// Extract comments
			comment := extractComment(line)
			if comment != "" {
				info.Comments = append(info.Comments, comment)
			}

			// Detect keywords and context
			for _, keyword := range bugKeywords {
				if strings.Contains(lineLower, keyword) {
					if !contains(info.Keywords, keyword) {
						info.Keywords = append(info.Keywords, keyword)
					}
					contextCounts["fix"]++
				}
			}

			for _, keyword := range validationKeywords {
				if strings.Contains(lineLower, keyword) {
					if !contains(info.Keywords, keyword) {
						info.Keywords = append(info.Keywords, keyword)
					}
					contextCounts["validation"]++
				}
			}

			for _, keyword := range apiKeywords {
				if strings.Contains(lineLower, keyword) {
					if !contains(info.Keywords, keyword) {
						info.Keywords = append(info.Keywords, keyword)
					}
					contextCounts["api"]++
				}
			}

			for _, keyword := range securityKeywords {
				if strings.Contains(lineLower, keyword) {
					if !contains(info.Keywords, keyword) {
						info.Keywords = append(info.Keywords, keyword)
					}
					contextCounts["security"]++
				}
			}

			for _, keyword := range performanceKeywords {
				if strings.Contains(lineLower, keyword) {
					if !contains(info.Keywords, keyword) {
						info.Keywords = append(info.Keywords, keyword)
					}
					contextCounts["performance"]++
				}
			}

			// Detect test-related content
			if strings.Contains(lineLower, "test") {
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

	// Determine primary context based on what we found
	maxContext := ""
	maxCount := 0
	for context, count := range contextCounts {
		if count > maxCount {
			maxCount = count
			maxContext = context
		}
	}
	info.Context = maxContext

	return info
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

func generateSmartCommitMessage(file, status string, diff DiffInfo, commitType string) string {
	fileName := filepath.Base(file)
	fileExt := filepath.Ext(file)
	baseName := strings.TrimSuffix(fileName, fileExt)

	// Use comments if they're descriptive
	if len(diff.Comments) > 0 {
		firstComment := diff.Comments[0]
		if len(firstComment) > 10 && len(firstComment) < 60 {
			lowerComment := strings.ToLower(firstComment)
			if strings.Contains(lowerComment, "fix") || strings.Contains(lowerComment, "add") ||
				strings.Contains(lowerComment, "update") || strings.Contains(lowerComment, "remove") {
				return firstComment
			}
		}
	}

	// Use context to create smarter messages
	var contextPrefix string
	switch diff.Context {
	case "fix":
		contextPrefix = "fix"
		commitType = "fix"
	case "validation":
		contextPrefix = "add validation for"
	case "api":
		contextPrefix = "add API endpoint for"
	case "security":
		contextPrefix = "improve security in"
	case "performance":
		contextPrefix = "optimize"
	}

	switch status {
	case "A ", " A":
		if contextPrefix != "" && len(diff.Functions) > 0 {
			return fmt.Sprintf("%s %s", contextPrefix, diff.Functions[0])
		}

		if len(diff.Functions) > 0 {
			if len(diff.Functions) == 1 {
				if len(diff.Variables) > 0 && contains(diff.Keywords, "error") {
					return fmt.Sprintf("add %s with error handling", diff.Functions[0])
				}
				return fmt.Sprintf("add %s", diff.Functions[0])
			}
			return fmt.Sprintf("add %s with %d functions", baseName, len(diff.Functions))
		}

		if len(diff.Imports) > 0 {
			return fmt.Sprintf("add %s with dependencies", baseName)
		}

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

	case "D ", " D":
		return fmt.Sprintf("remove %s", fileName)

	case "M ", " M", "MM":
		if contextPrefix != "" {
			if len(diff.Functions) > 0 {
				return fmt.Sprintf("%s %s", contextPrefix, diff.Functions[0])
			}
			return fmt.Sprintf("%s %s", contextPrefix, baseName)
		}

		if contains(diff.Keywords, "error") || contains(diff.Keywords, "bug") {
			if len(diff.Functions) > 0 {
				return fmt.Sprintf("fix error handling in %s", diff.Functions[0])
			}
			return fmt.Sprintf("fix error handling in %s", baseName)
		}

		if contains(diff.Keywords, "validate") || contains(diff.Keywords, "check") {
			if len(diff.Functions) > 0 {
				return fmt.Sprintf("add validation to %s", diff.Functions[0])
			}
			return fmt.Sprintf("add input validation to %s", baseName)
		}

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

		if len(diff.Functions) > 0 {
			if len(diff.Functions) == 1 {
				funcName := diff.Functions[0]
				if contains(diff.Keywords, "auth") || contains(diff.Keywords, "security") {
					return fmt.Sprintf("improve security in %s", funcName)
				}
				if contains(diff.Keywords, "optimize") || contains(diff.Keywords, "performance") {
					return fmt.Sprintf("optimize %s", funcName)
				}
				if commitType == "fix" {
					return fmt.Sprintf("fix %s", funcName)
				} else if commitType == "refactor" {
					return fmt.Sprintf("refactor %s", funcName)
				}
				return fmt.Sprintf("update %s", funcName)
			} else if len(diff.Functions) <= 3 {
				if commitType == "refactor" {
					return fmt.Sprintf("refactor %d functions in %s", len(diff.Functions), baseName)
				}
				return fmt.Sprintf("update %d functions in %s", len(diff.Functions), baseName)
			}
		}

		if len(diff.Imports) > 0 {
			if commitType == "feat" {
				return fmt.Sprintf("add dependencies to %s", baseName)
			}
			return fmt.Sprintf("update imports in %s", baseName)
		}

		if diff.LinesAdded > diff.LinesRemoved*3 {
			if commitType == "feat" {
				return fmt.Sprintf("extend %s functionality", baseName)
			}
		} else if diff.LinesRemoved > diff.LinesAdded*2 {
			if commitType == "refactor" {
				return fmt.Sprintf("simplify %s", baseName)
			}
			return fmt.Sprintf("clean up %s", baseName)
		}

		switch commitType {
		case "fix":
			return fmt.Sprintf("fix issues in %s", baseName)
		case "refactor":
			return fmt.Sprintf("refactor %s", baseName)
		case "feat":
			return fmt.Sprintf("enhance %s", baseName)
		default:
			return fmt.Sprintf("update %s", baseName)
		}

	default:
		return fmt.Sprintf("modify %s", fileName)
	}
}

func determineScope(file string) string {
	parts := strings.Split(file, "/")

	if len(parts) > 1 {
		firstDir := parts[0]

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

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func containsBugFixKeywords(diff DiffInfo) bool {
	return diff.LinesRemoved > 0 && diff.LinesAdded < diff.LinesRemoved
}

func extractVariableName(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "+") {
		trimmed = strings.TrimSpace(trimmed[1:])
	}

	patterns := []string{
		`var\s+(\w+)`,
		`const\s+(\w+)`,
		`let\s+(\w+)`,
		`(\w+)\s*:=`,
		`(\w+)\s*=`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(trimmed)
		if len(matches) > 1 {
			varName := matches[1]
			if len(varName) > 2 && varName != "err" && varName != "nil" && varName != "true" && varName != "false" {
				return varName
			}
		}
	}

	return ""
}

func extractComment(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "+") {
		trimmed = strings.TrimSpace(trimmed[1:])
	}

	if strings.Contains(trimmed, "//") {
		idx := strings.Index(trimmed, "//")
		comment := strings.TrimSpace(trimmed[idx+2:])
		if len(comment) > 5 {
			return comment
		}
	}

	if strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "#include") {
		comment := strings.TrimSpace(trimmed[1:])
		if len(comment) > 5 {
			return comment
		}
	}

	return ""
}

func isImportLine(line string) bool {
	trimmed := strings.TrimSpace(line[1:])
	return strings.HasPrefix(trimmed, "import ") ||
		strings.HasPrefix(trimmed, "from ") ||
		strings.HasPrefix(trimmed, "#include") ||
		strings.HasPrefix(trimmed, "require(") ||
		strings.HasPrefix(trimmed, "const ") && strings.Contains(trimmed, "require(") ||
		strings.HasPrefix(trimmed, "use ")
}

func extractImportName(line string) string {
	trimmed := strings.TrimSpace(line[1:])

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

	if strings.HasPrefix(trimmed, "from ") {
		parts := strings.Fields(trimmed)
		if len(parts) >= 2 {
			return parts[1]
		}
	}

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

	return ""
}

func formatConventionalCommit(commitType, scope, description string) string {
	if scope != "" {
		return fmt.Sprintf("%s(%s): %s", commitType, scope, description)
	}
	return fmt.Sprintf("%s: %s", commitType, description)
}

// ============================================================================
// GIT COMMAND EXECUTION
// ============================================================================

func executeGitCommand(repoPath string, args ...string) ([]byte, error) {
	maxRetries := 3
	retryDelay := 100 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		lockFile := filepath.Join(repoPath, ".git", "index.lock")
		if _, err := os.Stat(lockFile); err == nil {
			time.Sleep(retryDelay)
			continue
		}

		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}

		output, err := cmd.CombinedOutput()

		if err != nil && strings.Contains(string(output), "index.lock") {
			time.Sleep(retryDelay)
			retryDelay *= 2
			continue
		}

		return output, err
	}

	return nil, fmt.Errorf("git command failed after %d retries: index.lock conflict", maxRetries)
}

func parseGitStatus(statusText string) []GitChange {
	var changes []GitChange
	lines := strings.Split(string(statusText), "\n")

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		if len(line) < 3 {
			continue
		}

		status := line[:2]
		file := strings.TrimSpace(line[3:])

		change := GitChange{
			File:   file,
			Status: status,
		}

		changes = append(changes, change)
	}

	return changes
}

func parseGitStatusOutput(statusText string) (stagedFiles, unstagedFiles int, clean bool) {
	clean = statusText == ""
	if clean {
		return 0, 0, true
	}

	lines := strings.Split(statusText, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) >= 2 {
			stagedStatus := line[0]
			unstagedStatus := line[1]

			if stagedStatus != ' ' && stagedStatus != '?' {
				stagedFiles++
			}

			if unstagedStatus != ' ' {
				unstagedFiles++
			}
		}
	}

	return stagedFiles, unstagedFiles, false
}

func getBranchName(repoPath string) string {
	branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = repoPath
	branchOutput, err := branchCmd.Output()
	if err == nil {
		return strings.TrimSpace(string(branchOutput))
	}
	return "unknown"
}

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

// ============================================================================
// FILE STAGING OPERATIONS
// ============================================================================

func (m model) toggleStaging(filePath string) tea.Cmd {
	return func() tea.Msg {
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

		return tea.Batch(
			m.loadGitChanges(),
			m.loadGitStatus(),
			func() tea.Msg {
				return statusMsg{message: fmt.Sprintf("✅ %s: %s", strings.Title(action), filePath)}
			},
		)()
	}
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

func (m model) gitReset() tea.Cmd {
	return func() tea.Msg {
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

		output, err := executeGitCommand(m.repoPath, "reset", "HEAD")
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Git reset failed: %v - %s", err, string(output))}
		}
		return statusMsg{message: fmt.Sprintf("✅ Reset %d staged file(s)", stagedCount)}
	}
}

// ============================================================================
// COMMIT OPERATIONS
// ============================================================================

func (m model) commitWithMessage(message string) tea.Cmd {
	return func() tea.Msg {
		statusCmd := exec.Command("git", "diff", "--cached", "--name-only")
		statusCmd.Dir = m.repoPath
		statusOutput, err := statusCmd.Output()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to check staged changes: %v", err)}
		}

		if len(strings.TrimSpace(string(statusOutput))) == 0 {
			return statusMsg{message: "❌ No staged changes to commit. Stage files first."}
		}

		_, err = executeGitCommand(m.repoPath, "commit", "-m", message)
		if err != nil {
			return statusMsg{message: "❌ Commit failed - check commit message format"}
		}

		return statusMsg{message: fmt.Sprintf("✅ Committed: %s", message)}
	}
}

func (m model) validateCommitMessage(message string) bool {
	conventionalRegex := `^(feat|fix|docs|style|refactor|test|chore)(\(.+\))?: .{1,50}`
	matched, _ := regexp.MatchString(conventionalRegex, message)
	return matched
}

// ============================================================================
// BRANCH OPERATIONS
// ============================================================================

func (m model) switchBranch(branchName string) tea.Cmd {
	return func() tea.Msg {
		output, err := executeGitCommand(m.repoPath, "checkout", branchName)
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to switch branch: %v - %s", err, string(output))}
		}

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

// ============================================================================
// PUSH/PULL OPERATIONS
// ============================================================================

func (m model) gitPush() tea.Cmd {
	return func() tea.Msg {
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

		commitCmd := exec.Command("git", "log", "-1", "--oneline")
		commitCmd.Dir = m.repoPath
		commitOutput, _ := commitCmd.Output()
		lastCommit := strings.TrimSpace(string(commitOutput))

		cmd := exec.Command("git", "push")
		cmd.Dir = m.repoPath
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}

		output, err := cmd.CombinedOutput()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Git push failed: %v - %s", err, string(output))}
		}

		detailedOutput := fmt.Sprintf("Push Output:\n%s\n\nLast Commit:\n%s", string(output), lastCommit)

		return pushOutputMsg{
			output: detailedOutput,
			commit: lastCommit,
		}
	}
}

func (m model) gitPull() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "pull")
		cmd.Dir = m.repoPath
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}

		output, err := cmd.CombinedOutput()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Git pull failed: %v - %s", err, string(output))}
		}

		return tea.Batch(
			m.loadGitChanges(),
			m.loadGitStatus(),
			func() tea.Msg {
				return statusMsg{message: fmt.Sprintf("✅ Pull successful: %s", strings.TrimSpace(string(output)))}
			},
		)()
	}
}

func (m model) gitFetch() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "fetch")
		cmd.Dir = m.repoPath
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}

		output, err := cmd.CombinedOutput()
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Git fetch failed: %v - %s", err, string(output))}
		}

		return tea.Batch(
			m.loadGitStatus(),
			func() tea.Msg {
				return statusMsg{message: "✅ Fetched latest changes"}
			},
		)()
	}
}

// ============================================================================
// DIFF VIEWING
// ============================================================================

func (m model) viewDiff(filePath string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "diff", "--cached", filePath)
		cmd.Dir = m.repoPath
		output, err := cmd.Output()

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

func colorizeGitDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	var coloredLines []string

	for _, line := range lines {
		if len(line) == 0 {
			coloredLines = append(coloredLines, line)
			continue
		}

		switch {
		case strings.HasPrefix(line, "diff --git"):
			coloredLines = append(coloredLines, lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("226")).
				Render(line))

		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			coloredLines = append(coloredLines, lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("229")).
				Render(line))

		case strings.HasPrefix(line, "@@"):
			coloredLines = append(coloredLines, lipgloss.NewStyle().
				Foreground(lipgloss.Color("39")).
				Render(line))

		case strings.HasPrefix(line, "-"):
			coloredLines = append(coloredLines, lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")).
				Render(line))

		case strings.HasPrefix(line, "+"):
			coloredLines = append(coloredLines, lipgloss.NewStyle().
				Foreground(lipgloss.Color("46")).
				Render(line))

		case strings.HasPrefix(line, "index "):
			coloredLines = append(coloredLines, lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Render(line))

		default:
			coloredLines = append(coloredLines, lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Render(line))
		}
	}

	return strings.Join(coloredLines, "\n")
}

// ============================================================================
// CONFLICT RESOLUTION
// ============================================================================

func parseConflictMarkers(content string) []Conflict {
	var conflicts []Conflict
	lines := strings.Split(content, "\n")

	inConflict := false
	var currentConflict Conflict
	var oursContent []string
	var theirsContent []string
	inOurs := false
	lineStart := 0

	for i, line := range lines {
		if strings.HasPrefix(line, "<<<<<<< ") {
			inConflict = true
			inOurs = true
			lineStart = i
			oursContent = []string{}
			theirsContent = []string{}
		} else if strings.HasPrefix(line, "=======") && inConflict {
			inOurs = false
		} else if strings.HasPrefix(line, ">>>>>>> ") && inConflict {
			currentConflict = Conflict{
				LineStart:     lineStart,
				OursContent:   oursContent,
				TheirsContent: theirsContent,
			}
			conflicts = append(conflicts, currentConflict)
			inConflict = false
		} else if inConflict {
			if inOurs {
				oursContent = append(oursContent, line)
			} else {
				theirsContent = append(theirsContent, line)
			}
		}
	}

	return conflicts
}

func (m model) resolveConflict(index int, resolution string) tea.Cmd {
	return func() tea.Msg {
		if index >= len(m.conflicts) {
			return statusMsg{message: "❌ Invalid conflict index"}
		}

		conflict := m.conflicts[index]
		filePath := filepath.Join(m.repoPath, conflict.Path)

		content, err := os.ReadFile(filePath)
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to read file: %v", err)}
		}

		// This is simplified - in reality you'd need to properly handle the conflict markers
		var newContent string
		switch resolution {
		case "ours":
			newContent = string(content)
			newContent = regexp.MustCompile(`<<<<<<< .*?\n(.*?)\n=======\n.*?\n>>>>>>> .*?\n`).
				ReplaceAllString(newContent, "$1\n")
		case "theirs":
			newContent = string(content)
			newContent = regexp.MustCompile(`<<<<<<< .*?\n.*?\n=======\n(.*?)\n>>>>>>> .*?\n`).
				ReplaceAllString(newContent, "$1\n")
		case "both":
			newContent = string(content)
			newContent = regexp.MustCompile(`<<<<<<< .*?\n(.*?)\n=======\n(.*?)\n>>>>>>> .*?\n`).
				ReplaceAllString(newContent, "$1\n$2\n")
		}

		err = os.WriteFile(filePath, []byte(newContent), 0644)
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to write file: %v", err)}
		}

		// Stage the resolved file
		_, err = executeGitCommand(m.repoPath, "add", conflict.Path)
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to stage resolved file: %v", err)}
		}

		return tea.Batch(
			m.loadConflicts(),
			func() tea.Msg {
				return statusMsg{message: fmt.Sprintf("✅ Resolved conflict in %s", conflict.Path)}
			},
		)()
	}
}

func (m model) continueMerge() tea.Cmd {
	return func() tea.Msg {
		_, err := executeGitCommand(m.repoPath, "commit", "--no-edit")
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Failed to continue merge: %v", err)}
		}

		return tea.Batch(
			m.loadGitChanges(),
			m.loadGitStatus(),
			func() tea.Msg {
				return statusMsg{message: "✅ Merge completed successfully"}
			},
		)()
	}
}

func (m model) allConflictsResolved() bool {
	for _, conflict := range m.conflicts {
		if !conflict.IsResolved {
			return false
		}
	}
	return len(m.conflicts) == 0 || len(m.conflicts) > 0
}

// ============================================================================
// UNDO OPERATIONS
// ============================================================================

func (m model) softReset(count int) tea.Cmd {
	return func() tea.Msg {
		output, err := executeGitCommand(m.repoPath, "reset", "--soft", fmt.Sprintf("HEAD~%d", count))
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Soft reset failed: %v - %s", err, string(output))}
		}

		return tea.Batch(
			m.loadGitChanges(),
			m.loadGitStatus(),
			func() tea.Msg {
				return statusMsg{message: fmt.Sprintf("✅ Undid last %d commit(s), changes kept staged", count)}
			},
		)()
	}
}

func (m model) mixedReset(count int) tea.Cmd {
	return func() tea.Msg {
		output, err := executeGitCommand(m.repoPath, "reset", "--mixed", fmt.Sprintf("HEAD~%d", count))
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Mixed reset failed: %v - %s", err, string(output))}
		}

		return tea.Batch(
			m.loadGitChanges(),
			m.loadGitStatus(),
			func() tea.Msg {
				return statusMsg{message: fmt.Sprintf("✅ Undid last %d commit(s), changes unstaged", count)}
			},
		)()
	}
}

func (m model) hardReset(count int) tea.Cmd {
	return func() tea.Msg {
		output, err := executeGitCommand(m.repoPath, "reset", "--hard", fmt.Sprintf("HEAD~%d", count))
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Hard reset failed: %v - %s", err, string(output))}
		}

		return tea.Batch(
			m.loadGitChanges(),
			m.loadGitStatus(),
			func() tea.Msg {
				return statusMsg{message: fmt.Sprintf("⚠️ Hard reset: undid %d commit(s) and DISCARDED all changes", count)}
			},
		)()
	}
}

// ============================================================================
// REBASE OPERATIONS
// ============================================================================

func (m model) executeRebase() tea.Cmd {
	return func() tea.Msg {
		// This is a simplified version - real interactive rebase is complex
		return statusMsg{message: "⚠️ Interactive rebase not yet implemented in TUI. Use git CLI."}
	}
}

// ============================================================================
// TABLE UPDATE FUNCTIONS
// ============================================================================

func (m *model) updateFilesTable() {
	var rows []table.Row
	for _, change := range m.changes {
		status := getStatusIcon(change.Status)
		row := table.Row{
			status,
			change.File,
		}
		rows = append(rows, row)
	}
	m.filesTable.SetRows(rows)
}

func (m *model) updateBranchesTable() {
	var rows []table.Row
	for _, branch := range m.branches {
		current := ""
		if branch.IsCurrent {
			current = "✓ "
		}

		status := ""
		if branch.Ahead > 0 && branch.Behind > 0 {
			status = fmt.Sprintf("↑%d ↓%d", branch.Ahead, branch.Behind)
		} else if branch.Ahead > 0 {
			status = fmt.Sprintf("↑%d ahead", branch.Ahead)
		} else if branch.Behind > 0 {
			status = fmt.Sprintf("↓%d behind", branch.Behind)
		}

		row := table.Row{
			current + branch.Name,
			status,
			branch.Upstream,
		}
		rows = append(rows, row)
	}
	m.branchesTable.SetRows(rows)
}

func (m *model) updateToolsTable() {
	rows := []table.Row{
		{"Undo Commits", "Soft/mixed/hard reset to undo commits"},
		{"Interactive Rebase", "Reorder, squash, or edit commits"},
		{"Commit History", "View detailed commit log"},
		{"Remote Operations", "Push, pull, fetch from remote"},
	}
	m.toolsTable.SetRows(rows)
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

func (m *model) updateConflictsTable() {
	var rows []table.Row
	for _, conflict := range m.conflicts {
		status := "Unresolved"
		if conflict.IsResolved {
			status = "Resolved"
		}
		row := table.Row{
			conflict.Path,
			fmt.Sprintf("%d", len(conflict.Conflicts)),
			status,
		}
		rows = append(rows, row)
	}
	m.conflictsTable.SetRows(rows)
}

func (m *model) updateComparisonTable() {
	if m.branchComparison == nil {
		return
	}

	var rows []table.Row
	for _, commit := range m.branchComparison.AheadCommits {
		row := table.Row{
			"Ahead",
			commit.Hash,
			commit.Message,
		}
		rows = append(rows, row)
	}
	for _, commit := range m.branchComparison.BehindCommits {
		row := table.Row{
			"Behind",
			commit.Hash,
			commit.Message,
		}
		rows = append(rows, row)
	}
	m.comparisonTable.SetRows(rows)
}

func (m *model) updateRebaseTable() {
	var rows []table.Row
	for _, commit := range m.rebaseCommits {
		row := table.Row{
			commit.Action,
			commit.Hash,
			commit.Message,
		}
		rows = append(rows, row)
	}
	m.rebaseTable.SetRows(rows)
}

func getStatusIcon(status string) string {
	if len(status) < 2 {
		return "📄 Unknown"
	}

	staged := status[0]
	unstaged := status[1]

	var action string
	var icon string

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
	case '?':
		icon = "❓"
		action = "Untracked"
	default:
		icon = "📄"
		action = "Changed"
	}

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

// ============================================================================
// HELPER FUNCTIONS - Refresh
// ============================================================================

func (m model) refreshAfterCommit() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(50 * time.Millisecond)
		return tea.Batch(
			m.loadGitStatus(),
			m.loadGitChanges(),
			m.loadRecentCommits(),
		)()
	}
}

func (m model) refreshAfterStaging() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(50 * time.Millisecond)
		return tea.Batch(
			m.loadGitStatus(),
			m.loadGitChanges(),
		)()
	}
}

func (m model) hasConflicts() bool {
	for _, change := range m.changes {
		if strings.Contains(change.Status, "U") || strings.Contains(change.Status, "A") && strings.Contains(change.Status, "A") {
			return true
		}
	}
	return false
}

func (m *model) adjustTableSizes() {
	if m.height <= 0 {
		return
	}

	availableHeight := m.height - 10
	if availableHeight < 5 {
		availableHeight = 5
	}

	m.filesTable.SetHeight(availableHeight)
	m.branchesTable.SetHeight(availableHeight)
	m.toolsTable.SetHeight(availableHeight / 2)
	m.historyTable.SetHeight(availableHeight)
	m.conflictsTable.SetHeight(availableHeight)
	m.comparisonTable.SetHeight(availableHeight)
	m.rebaseTable.SetHeight(availableHeight)

	// Adjust widths
	if m.width > 0 {
		availableWidth := m.width - 6

		filesColumns := []table.Column{
			{Title: "Status", Width: 25},
			{Title: "File", Width: availableWidth - 30},
		}
		m.filesTable.SetColumns(filesColumns)

		branchesColumns := []table.Column{
			{Title: "Branch", Width: availableWidth / 3},
			{Title: "Status", Width: availableWidth / 4},
			{Title: "Upstream", Width: availableWidth / 3},
		}
		m.branchesTable.SetColumns(branchesColumns)
	}
}

// ============================================================================
// VIEW RENDERING
// ============================================================================

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	// Header with tabs
	header := m.renderHeader()

	// Content based on current tab
	var content string
	switch m.tab {
	case "workspace":
		content = m.renderWorkspaceTab()
	case "commit":
		content = m.renderCommitTab()
	case "branches":
		content = m.renderBranchesTab()
	case "tools":
		content = m.renderToolsTab()
	}

	// Footer with help
	footer := m.renderFooter()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		content,
		"",
		footer,
	)
}

func (m model) renderHeader() string {
	// Title
	title := titleStyle.Render("🚀 Git Helper - Redesigned")

	// Repo info
	repoInfo := lipgloss.NewStyle().
		Foreground(lipgloss.Color("208")).
		Render(fmt.Sprintf(" %s", filepath.Base(m.repoPath)))

	// Git status bar
	statusBar := m.renderGitStatusBar()

	// Tabs
	tab1 := m.renderTabButton("1", "Workspace", m.tab == "workspace")
	tab2 := m.renderTabButton("2", "Commit", m.tab == "commit")
	tab3 := m.renderTabButton("3", "Branches", m.tab == "branches")
	tab4 := m.renderTabButton("4", "Tools", m.tab == "tools")

	tabsLine := lipgloss.JoinHorizontal(lipgloss.Top, tab1, tab2, tab3, tab4)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, title, repoInfo),
		statusBar,
		tabsLine,
	)
}

func (m model) renderTabButton(key, label string, active bool) string {
	style := tabStyle
	if active {
		style = activeTabStyle
	}
	return style.Render(fmt.Sprintf("[%s] %s", key, label))
}

func (m model) renderGitStatusBar() string {
	branchIcon := "🌿"
	cleanIcon := "✅"
	dirtyIcon := "🔴"
	stagedIcon := "🟢"
	emptyIcon := "⚪"
	aheadIcon := "⬆️"
	behindIcon := "⬇️"

	branchInfo := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).
		Render(fmt.Sprintf("%s %s", branchIcon, m.gitState.Branch))

	var stagingStatus string
	if m.gitState.StagedFiles == 0 {
		stagingStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
			Render(fmt.Sprintf("%s Nothing staged", emptyIcon))
	} else {
		stagingStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).
			Render(fmt.Sprintf("%s %d staged", stagedIcon, m.gitState.StagedFiles))
	}

	var workingDirStatus string
	if m.gitState.UnstagedFiles > 0 {
		workingDirStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).
			Render(fmt.Sprintf("%s %d unstaged", dirtyIcon, m.gitState.UnstagedFiles))
	} else {
		workingDirStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).
			Render(fmt.Sprintf("%s Clean", cleanIcon))
	}

	var syncInfo []string
	if m.gitState.Ahead > 0 {
		syncInfo = append(syncInfo, lipgloss.NewStyle().Foreground(lipgloss.Color("117")).
			Render(fmt.Sprintf("%s %d", aheadIcon, m.gitState.Ahead)))
	}
	if m.gitState.Behind > 0 {
		syncInfo = append(syncInfo, lipgloss.NewStyle().Foreground(lipgloss.Color("173")).
			Render(fmt.Sprintf("%s %d", behindIcon, m.gitState.Behind)))
	}

	elements := []string{branchInfo, stagingStatus, workingDirStatus}
	elements = append(elements, syncInfo...)

	return lipgloss.NewStyle().
		Background(lipgloss.Color("235")).
		Padding(0, 1).
		Render(strings.Join(elements, " • "))
}

func (m model) renderWorkspaceTab() string {
	if m.viewMode == "conflicts" {
		return m.renderConflictsView()
	}

	if m.viewMode == "diff" {
		return m.renderDiffView()
	}

	// Files view
	var content string
	if len(m.changes) == 0 {
		content = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
			Render("No changes found. Make some changes or stage files.")
	} else {
		content = m.filesTable.View()

		// Show diff preview in bottom panel if enabled
		if m.showDiffPreview && len(m.changes) > 0 {
			selectedIndex := m.filesTable.Cursor()
			if selectedIndex < len(m.changes) && m.diffContent != "" {
				diffPreview := lipgloss.NewStyle().
					Border(lipgloss.NormalBorder()).
					BorderForeground(lipgloss.Color("240")).
					Padding(1).
					Height(10).
					Render(colorizeGitDiff(m.diffContent))

				content = lipgloss.JoinVertical(lipgloss.Left, content, "", diffPreview)
			}
		}
	}

	return content
}

func (m model) renderConflictsView() string {
	if len(m.conflicts) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
			Render("No conflicts found.")
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("196")).
		Render("⚠️ Merge Conflicts")

	return lipgloss.JoinVertical(lipgloss.Left, header, "", m.conflictsTable.View())
}

func (m model) renderDiffView() string {
	if m.diffContent == "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
			Render("No diff to display.")
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Render("File Diff (press ESC to go back)")

	return lipgloss.JoinVertical(lipgloss.Left, header, "", colorizeGitDiff(m.diffContent))
}

func (m model) renderCommitTab() string {
	// Header
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		MarginBottom(1).
		Render(fmt.Sprintf("📝 Commit Changes (%d files staged)", m.gitState.StagedFiles))

	if m.gitState.StagedFiles == 0 {
		msg := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("No files staged. Go to Workspace tab and stage files first.")
		return lipgloss.JoinVertical(lipgloss.Left, header, "", msg)
	}

	// Recent commits
	var recentCommitsSection string
	if len(m.recentCommits) > 0 {
		recentCommitsSection = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("Recent commits:")
		for _, commit := range m.recentCommits {
			recentCommitsSection += "\n  " + lipgloss.NewStyle().
				Foreground(lipgloss.Color("39")).
				Render(commit.Hash) + " " + commit.Message
		}
	}

	// Suggestions (use arrow keys to navigate, or 1-9 for instant commit)
	var suggestionsSection string
	if len(m.suggestions) > 0 {
		suggestionsSection = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Render("\nSuggestions:")

		for i, suggestion := range m.suggestions {
			if i >= 9 {
				break
			}
			// Show visual indicator for selected suggestion
			indicator := "  "
			if m.selectedSuggestion == i+1 {
				indicator = "→ "
			}

			style := suggestionStyle
			if m.selectedSuggestion == i+1 {
				style = selectedSuggestionStyle
			}
			suggestionsSection += "\n" + style.Render(fmt.Sprintf("%s%s", indicator, suggestion.Message))
		}
	}

	// Custom input
	customSection := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Render("\n\nCustom message:")
	customSection += "\n" + m.commitInput.View()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		recentCommitsSection,
		suggestionsSection,
		customSection,
	)
}

func (m model) renderBranchesTab() string {
	if m.branchComparison != nil {
		return m.renderBranchComparison()
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		MarginBottom(1).
		Render("🌿 Branches")

	if m.branchInput.Focused() {
		inputLabel := lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			Render("Create new branch:")
		return lipgloss.JoinVertical(lipgloss.Left, header, "", inputLabel, m.branchInput.View())
	}

	if len(m.branches) == 0 {
		msg := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("Loading branches...")
		return lipgloss.JoinVertical(lipgloss.Left, header, "", msg)
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, "", m.branchesTable.View())
}

func (m model) renderBranchComparison() string {
	if m.branchComparison == nil {
		return ""
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Render(fmt.Sprintf("Branch Comparison: %s vs %s",
			m.branchComparison.SourceBranch,
			m.branchComparison.TargetBranch))

	summary := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render(fmt.Sprintf("\n%d commits ahead, %d commits behind, %d files changed",
			len(m.branchComparison.AheadCommits),
			len(m.branchComparison.BehindCommits),
			len(m.branchComparison.DifferingFiles)))

	return lipgloss.JoinVertical(lipgloss.Left, header, summary, "", m.comparisonTable.View())
}

func (m model) renderToolsTab() string {
	switch m.toolMode {
	case "menu":
		return m.renderToolsMenu()
	case "undo":
		return m.renderUndoView()
	case "rebase":
		return m.renderRebaseView()
	case "history":
		return m.renderHistoryView()
	case "remote":
		return m.renderRemoteView()
	}
	return ""
}

func (m model) renderToolsMenu() string {
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		MarginBottom(1).
		Render("🛠️ Tools")

	return lipgloss.JoinVertical(lipgloss.Left, header, "", m.toolsTable.View())
}

func (m model) renderUndoView() string {
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("196")).
		MarginBottom(1).
		Render("↩️ Undo Commits")

	return lipgloss.JoinVertical(lipgloss.Left, header, "", m.undoTable.View())
}

func (m model) renderRebaseView() string {
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Render("🔀 Interactive Rebase")

	if m.rebaseInput.Focused() {
		inputLabel := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("Number of commits to rebase:")
		return lipgloss.JoinVertical(lipgloss.Left, header, "", inputLabel, m.rebaseInput.View())
	}

	if len(m.rebaseCommits) == 0 {
		msg := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("Enter number of commits to rebase (1-50)")
		return lipgloss.JoinVertical(lipgloss.Left, header, "", msg)
	}

	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render("p=pick, s=squash, r=reword, d=drop, f=fixup, enter=execute")

	return lipgloss.JoinVertical(lipgloss.Left, header, "", m.rebaseTable.View(), "", help)
}

func (m model) renderHistoryView() string {
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		MarginBottom(1).
		Render("📜 Commit History")

	if len(m.commits) == 0 {
		msg := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("Loading commit history...")
		return lipgloss.JoinVertical(lipgloss.Left, header, "", msg)
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, "", m.historyTable.View())
}

func (m model) renderRemoteView() string {
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Render("🌐 Remote Operations")

	options := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render(`
[p] Push  - Push commits to remote
[l] Pull  - Pull changes from remote
[f] Fetch - Fetch remote changes without merging
`)

	if m.pushOutput != "" {
		output := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(1).
			Render(m.pushOutput)
		return lipgloss.JoinVertical(lipgloss.Left, header, options, "", output)
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, options)
}

// formatHelp takes key=description pairs and formats them with colors
func formatHelp(pairs ...string) string {
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var parts []string
	for _, pair := range pairs {
		// Split on '=' to get key and description
		split := strings.SplitN(pair, "=", 2)
		if len(split) == 2 {
			formatted := keyStyle.Render(split[0]) + sepStyle.Render("=") + descStyle.Render(split[1])
			parts = append(parts, formatted)
		} else {
			// No '=' found, just render as-is
			parts = append(parts, descStyle.Render(pair))
		}
	}
	return strings.Join(parts, sepStyle.Render("  "))
}

func (m model) renderFooter() string {
	var help string

	switch m.tab {
	case "workspace":
		if m.viewMode == "conflicts" {
			help = formatHelp("o=ours", "t=theirs", "b=both", "c=continue", "esc=back", "r=refresh", "1-4=tabs", "q=quit")
		} else if m.viewMode == "diff" {
			help = formatHelp("esc=back to files", "q=quit")
		} else {
			help = formatHelp("space=stage/unstage", "a=add all", "r=refresh", "v=toggle diff", "d=view diff", "R=reset", "1-4=tabs", "q=quit")
		}
	case "commit":
		if m.commitInput.Focused() {
			help = formatHelp("enter=commit", "esc=cancel")
		} else {
			help = formatHelp("↑/↓=navigate", "enter/space=commit", "c=custom", "1-4=tabs", "q=quit")
		}
	case "branches":
		if m.branchInput.Focused() {
			help = formatHelp("enter=create", "esc=cancel")
		} else if m.branchComparison != nil {
			help = formatHelp("esc=back to branches", "1-4=tabs", "q=quit")
		} else {
			help = formatHelp("enter=switch", "n=new", "d=delete", "c=compare", "r=refresh", "1-4=tabs", "q=quit")
		}
	case "tools":
		switch m.toolMode {
		case "menu":
			help = formatHelp("↑/↓=navigate", "enter=select", "esc=back", "1-4=tabs", "q=quit")
		case "undo":
			help = formatHelp("↑/↓=navigate", "enter=select", "y=confirm", "esc=back", "q=quit")
		case "rebase":
			if m.rebaseInput.Focused() {
				help = formatHelp("enter=load commits", "esc=cancel")
			} else {
				help = formatHelp("p=pick", "s=squash", "r=reword", "d=drop", "f=fixup", "enter=execute", "esc=back", "q=quit")
			}
		case "history":
			help = formatHelp("r=refresh", "c=copy hash", "esc=back", "q=quit")
		case "remote":
			help = formatHelp("p=push", "l=pull", "f=fetch", "esc=back", "q=quit")
		}
	}

	// Add status message if present
	if m.statusMsg != "" && time.Now().Before(m.statusExpiry) {
		statusLine := lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			Bold(true).
			Render("\n" + m.statusMsg)
		return help + statusLine
	}

	return help
}

// ============================================================================
// MAIN FUNCTION
// ============================================================================

func main() {
	repoPath, err := os.Getwd()
	if err != nil {
		log.Fatal("Failed to get current directory:", err)
	}

	// Verify we're in a git repo
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		log.Fatal("Not in a git repository. Please run this from a git repository.")
	}

	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
