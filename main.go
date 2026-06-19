package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

type PnpmWorkspace struct {
	Packages []string `yaml:"packages"`
}

type DependencyItem struct {
	Name    string
	Version string
	Latest  string
	IsDev   bool
}

type PackageJSON struct {
	Name            string            `json:"name"`
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Workspaces      any               `json:"workspaces"`
}

type ProjectModule struct {
	Name string
	Path string
}

type Config struct {
	Theme int    `json:"theme"`
	Lang  string `json:"lang"`
}

type panel int

const (
	packagesPanel panel = iota
	tasksPanel
	dependenciesPanel
)

type Theme struct {
	Focus  lipgloss.Color
	Normal lipgloss.Color
	Accent lipgloss.Color
}

var themes = []struct {
	Name  string
	Theme Theme
}{
	{"Tokyo Night", Theme{Focus: lipgloss.Color("62"), Normal: lipgloss.Color("240"), Accent: lipgloss.Color("14")}},
	{"Dracula", Theme{Focus: lipgloss.Color("212"), Normal: lipgloss.Color("61"), Accent: lipgloss.Color("84")}},
	{"Gruvbox", Theme{Focus: lipgloss.Color("214"), Normal: lipgloss.Color("244"), Accent: lipgloss.Color("142")}},
	{"Nord", Theme{Focus: lipgloss.Color("44"), Normal: lipgloss.Color("242"), Accent: lipgloss.Color("39")}},
	{"Catppuccin", Theme{Focus: lipgloss.Color("117"), Normal: lipgloss.Color("243"), Accent: lipgloss.Color("218")}},
	{"One Dark", Theme{Focus: lipgloss.Color("170"), Normal: lipgloss.Color("239"), Accent: lipgloss.Color("114")}},
	{"Monokai Pro", Theme{Focus: lipgloss.Color("197"), Normal: lipgloss.Color("241"), Accent: lipgloss.Color("148")}},
	{"Night Owl", Theme{Focus: lipgloss.Color("201"), Normal: lipgloss.Color("238"), Accent: lipgloss.Color("45")}},
	{"Rose Pine", Theme{Focus: lipgloss.Color("210"), Normal: lipgloss.Color("242"), Accent: lipgloss.Color("116")}},
	{"Cyberpunk", Theme{Focus: lipgloss.Color("207"), Normal: lipgloss.Color("239"), Accent: lipgloss.Color("226")}},
	{"Everforest", Theme{Focus: lipgloss.Color("108"), Normal: lipgloss.Color("243"), Accent: lipgloss.Color("223")}},
}

var (
	version     = "dev"
	commit      = "none"
	date        = "unknown"
	buildSource = "source"
)

var languageNames = map[string]string{
	"en_us": "English (US)",
	"es_ar": "Español (AR)",
	"fr_fr": "Français (FR)",
	"fr_ch": "Français (CH)",
	"de_de": "Deutsch (DE)",
}

type fetchedDepsMsg []DependencyItem
type scriptFinishedMsg struct{ err error }
type updateFinishedMsg struct{ err error }

type model struct {
	activeMod       int
	activeScript    int
	activeDep       int
	width           int
	height          int
	depScrollStart  int
	currentLangIdx  int
	currentThemeIdx int
	settingsCursor  int
	showSettings    bool
	isLoadingDeps   bool
	isRunningTask   bool
	activePanel     panel
	errorMsg        string
	statusMsg       string
	currentScripts  []string
	availableLangs  []string
	localizer       *Localizer
	modules         []ProjectModule
	currentDeps     []DependencyItem
	scriptsMap      map[string]string
}

func getConfigPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	lazypkgDir := filepath.Join(configDir, "lazypkg")
	if err := os.MkdirAll(lazypkgDir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(lazypkgDir, "config.json"), nil
}

func loadConfig() Config {
	defaultConfig := Config{Theme: 0, Lang: "en_us"}
	path, err := getConfigPath()
	if err != nil {
		return defaultConfig
	}
	file, err := os.ReadFile(path)
	if err != nil {
		return defaultConfig
	}
	var cfg Config
	if err := json.Unmarshal(file, &cfg); err != nil {
		return defaultConfig
	}
	return cfg
}

func saveConfig(themeIdx int, lang string) {
	path, err := getConfigPath()
	if err != nil {
		return
	}
	cfg := Config{
		Theme: themeIdx,
		Lang:  lang,
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0644)
}

func findModules() ([]ProjectModule, error) {
	var modules []ProjectModule
	var workspacePatterns []string

	pnpmFile, err := os.ReadFile("pnpm-workspace.yaml")
	if err == nil {
		var ws PnpmWorkspace
		if err := yaml.Unmarshal(pnpmFile, &ws); err == nil && len(ws.Packages) > 0 {
			workspacePatterns = ws.Packages
		}
	}

	if len(workspacePatterns) == 0 {
		rootPkgFile, err := os.ReadFile("package.json")
		if err == nil {
			var rootPkg PackageJSON
			if err := json.Unmarshal(rootPkgFile, &rootPkg); err == nil && rootPkg.Workspaces != nil {
				if patterns, ok := rootPkg.Workspaces.([]any); ok {
					for _, p := range patterns {
						if str, ok := p.(string); ok {
							workspacePatterns = append(workspacePatterns, str)
						}
					}
				} else if mapWorkspaces, ok := rootPkg.Workspaces.(map[string]any); ok {
					if pkgs, exists := mapWorkspaces["packages"]; exists {
						if patterns, ok := pkgs.([]any); ok {
							for _, p := range patterns {
								if str, ok := p.(string); ok {
									workspacePatterns = append(workspacePatterns, str)
								}
							}
						}
					}
				}
			}
		}
	}

	if len(workspacePatterns) > 0 {
		for _, pattern := range workspacePatterns {
			matches, _ := filepath.Glob(pattern)
			for _, match := range matches {
				pkgPath := filepath.Join(match, "package.json")
				if msg, err := readPackageName(pkgPath); err == nil {
					modules = append(modules, ProjectModule{Name: msg, Path: match})
				}
			}
		}
		if len(modules) > 0 {
			return modules, nil
		}
	}

	files, err := os.ReadDir(".")
	if err != nil {
		return nil, err
	}

	if msg, err := readPackageName("package.json"); err == nil {
		modules = append(modules, ProjectModule{Name: msg + " (root)", Path: "."})
	}

	for _, file := range files {
		if file.IsDir() && file.Name() != "node_modules" && file.Name() != ".git" {
			pkgPath := filepath.Join(file.Name(), "package.json")
			if msg, err := readPackageName(pkgPath); err == nil {
				modules = append(modules, ProjectModule{Name: msg, Path: file.Name()})
			}
		}
	}

	return modules, nil
}

func getPackageManager(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "pnpm-lock.yaml")); err == nil {
		return "pnpm"
	}
	if _, err := os.Stat(filepath.Join(dir, "bun.lock")); err == nil {
		return "bun"
	}
	if _, err := os.Stat(filepath.Join(dir, "yarn.lock")); err == nil {
		return "yarn"
	}
	return "npm"
}

func readPackageName(path string) (string, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var pkg PackageJSON
	if err := json.Unmarshal(file, &pkg); err != nil {
		return "", err
	}
	if pkg.Name == "" {
		return filepath.Base(filepath.Dir(path)), nil
	}
	return pkg.Name, nil
}

func (m *model) loadScriptsForActiveModule() {
	if len(m.modules) == 0 {
		return
	}

	mod := m.modules[m.activeMod]
	pkgPath := filepath.Join(mod.Path, "package.json")
	m.currentScripts = []string{}
	m.scriptsMap = make(map[string]string)
	m.activeScript = 0

	file, err := os.ReadFile(pkgPath)
	if err != nil {
		return
	}
	var pkg PackageJSON
	if err := json.Unmarshal(file, &pkg); err != nil {
		return
	}
	for k, v := range pkg.Scripts {
		m.currentScripts = append(m.currentScripts, k)
		m.scriptsMap[k] = v
	}
	sort.Strings(m.currentScripts)
}

func fetchLatestVersions(dir string, deps []DependencyItem) tea.Cmd {
	return func() tea.Msg {
		var checkedDeps []DependencyItem
		for _, dep := range deps {
			item := dep
			if dep.Latest != "fetching..." && dep.Latest != "" && dep.Latest != "unknown" {
				checkedDeps = append(checkedDeps, dep)
				continue
			}

			cmd := exec.Command("npm", "view", dep.Name, "version")
			cmd.Dir = dir

			var out bytes.Buffer
			cmd.Stdout = &out

			if err := cmd.Run(); err == nil {
				item.Latest = strings.TrimSpace(out.String())
			} else {
				item.Latest = "unknown"
			}
			checkedDeps = append(checkedDeps, item)
		}
		return fetchedDepsMsg(checkedDeps)
	}
}

func runProjectScript(dir string, scriptName string) tea.Cmd {
	pkgManager := getPackageManager(dir)

	var cmd *exec.Cmd
	switch pkgManager {
	case "pnpm":
		cmd = exec.Command("pnpm", "--filter", ".", "run", scriptName)
	case "npm":
		cmd = exec.Command("npm", "run", scriptName)
	case "bun":
		cmd = exec.Command("bun", "run", scriptName)
	case "yarn":
		cmd = exec.Command("yarn", "run", scriptName)
	}

	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return scriptFinishedMsg{err: err}
	})
}

func updateDependency(dir string, depName string, isDev bool) tea.Cmd {
	pkgManager := getPackageManager(dir)

	var args []string
	switch pkgManager {
	case "pnpm":
		if isDev {
			args = []string{"install", "-D", depName + "@latest"}
		} else {
			args = []string{"install", depName + "@latest"}
		}
	case "npm":
		if isDev {
			args = []string{"install", "-D", depName + "@latest"}
		} else {
			args = []string{"install", depName + "@latest"}
		}
	case "bun":
		if isDev {
			args = []string{"install", "-D", depName + "@latest"}
		} else {
			args = []string{"install", depName + "@latest"}
		}
	case "yarn":
		if isDev {
			args = []string{"install", "-D", depName + "@latest"}
		} else {
			args = []string{"install", depName + "@latest"}
		}
	}

	cmd := exec.Command(pkgManager, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return updateFinishedMsg{err: err}
	})
}

func (m *model) loadDependenciesForActiveModule() tea.Cmd {
	m.activeDep = 0
	m.depScrollStart = 0
	m.currentDeps = []DependencyItem{}

	if len(m.modules) == 0 {
		return nil
	}

	mod := m.modules[m.activeMod]
	pkgPath := filepath.Join(mod.Path, "package.json")

	file, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil
	}

	var pkg PackageJSON
	if err := json.Unmarshal(file, &pkg); err != nil {
		return nil
	}

	var localDeps []DependencyItem
	for name, ver := range pkg.Dependencies {
		localDeps = append(localDeps, DependencyItem{
			Name:    name,
			Version: ver,
			Latest:  "fetching...",
			IsDev:   false,
		})
	}

	for name, ver := range pkg.DevDependencies {
		localDeps = append(localDeps, DependencyItem{
			Name:    name,
			Version: ver,
			Latest:  "fetching...",
			IsDev:   true,
		})
	}

	sort.Slice(localDeps, func(i, j int) bool {
		return localDeps[i].Name < localDeps[j].Name
	})

	m.currentDeps = localDeps
	m.isLoadingDeps = true
	return fetchLatestVersions(mod.Path, localDeps)
}

func initialModel() (model, tea.Cmd) {
	loc := NewLocalizer()
	cfg := loadConfig()
	loc.SetLanguage(cfg.Lang)

	mods, err := findModules()
	var errStr string
	if err != nil {
		errStr = err.Error()
	}

	availableLangs := []string{
		"en_us",
		"es_ar",
		"de_de",
		"fr_fr",
		"fr_ch",
	}

	langIdx := 0
	for i, l := range availableLangs {
		if l == cfg.Lang {
			langIdx = i
			break
		}
	}

	if len(mods) == 0 {
		errStr = loc.T("error_no_modules")
	}

	m := model{
		activePanel:     packagesPanel,
		modules:         mods,
		activeMod:       0,
		errorMsg:        errStr,
		localizer:       loc,
		showSettings:    false,
		settingsCursor:  0,
		availableLangs:  availableLangs,
		currentLangIdx:  langIdx,
		currentThemeIdx: cfg.Theme, // Aplicar tema guardado
	}
	m.loadScriptsForActiveModule()
	cmd := m.loadDependenciesForActiveModule()
	return m, cmd
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case fetchedDepsMsg:
		m.isLoadingDeps = false
		m.currentDeps = msg
		return m, nil

	case scriptFinishedMsg:
		m.isRunningTask = false
		m.statusMsg = ""
		if msg.err != nil {
			m.errorMsg = msg.err.Error()
		}
		return m, tea.ClearScreen

	case updateFinishedMsg:
		m.isRunningTask = false
		m.statusMsg = ""
		if msg.err != nil {
			m.errorMsg = msg.err.Error()
			return m, nil
		}
		return m, tea.Batch(tea.ClearScreen, m.loadDependenciesForActiveModule())

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if !m.showSettings {
				return m, tea.Quit
			}

		case "o":
			m.showSettings = !m.showSettings
			return m, nil
		}

		if m.showSettings {
			switch msg.String() {
			case "j", "down":
				if m.settingsCursor < 1 {
					m.settingsCursor++
				}
			case "k", "up":
				if m.settingsCursor > 0 {
					m.settingsCursor--
				}
			case "l", "right":
				if m.settingsCursor == 0 {
					m.currentLangIdx = (m.currentLangIdx + 1) % len(m.availableLangs)
					m.localizer.SetLanguage(m.availableLangs[m.currentLangIdx])
				} else {
					m.currentThemeIdx = (m.currentThemeIdx + 1) % len(themes)
				}
			case "h", "left":
				if m.settingsCursor == 0 {
					m.currentLangIdx = (m.currentLangIdx - 1 + len(m.availableLangs)) % len(m.availableLangs)
					m.localizer.SetLanguage(m.availableLangs[m.currentLangIdx])
				} else {
					m.currentThemeIdx = (m.currentThemeIdx - 1 + len(themes)) % len(themes)
				}
			case "s":
				saveConfig(m.currentThemeIdx, m.availableLangs[m.currentLangIdx])
				m.showSettings = false
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "tab":
			m.activePanel = (m.activePanel + 1) % 3
			return m, nil

		case "j", "down":
			switch m.activePanel {
			case packagesPanel:
				if m.activeMod < len(m.modules)-1 {
					m.activeMod++
					m.loadScriptsForActiveModule()
					cmd := m.loadDependenciesForActiveModule()
					return m, cmd
				}
			case tasksPanel:
				if m.activeScript < len(m.currentScripts)-1 {
					m.activeScript++
				}
			case dependenciesPanel:
				if m.activeDep < len(m.currentDeps)-1 {
					m.activeDep++

					availHeight := max(12, m.height-4)
					rightPanelHeight := (availHeight / 3) - 1
					depsPanelHeight := availHeight - rightPanelHeight - 4
					maxRows := depsPanelHeight - 3

					if m.activeDep >= m.depScrollStart+maxRows {
						m.depScrollStart++
					}
				}
			}
			return m, nil

		case "k", "up":
			switch m.activePanel {
			case packagesPanel:
				if m.activeMod > 0 {
					m.activeMod--
					m.loadScriptsForActiveModule()
					cmd := m.loadDependenciesForActiveModule()
					return m, cmd
				}
			case tasksPanel:
				if m.activeScript > 0 {
					m.activeScript--
				}
			case dependenciesPanel:
				if m.activeDep > 0 {
					m.activeDep--

					if m.activeDep < m.depScrollStart {
						m.depScrollStart--
					}
				}
			}
			return m, nil

		case "enter", "r":
			if m.activePanel == tasksPanel && len(m.currentScripts) > 0 {
				m.isRunningTask = true
				m.statusMsg = "Running script..."
				return m, runProjectScript(m.modules[m.activeMod].Path, m.currentScripts[m.activeScript])
			}

		case "u":
			if m.activePanel == dependenciesPanel && len(m.currentDeps) > 0 {
				m.isRunningTask = true
				m.statusMsg = "Updating dependency..."
				target := m.currentDeps[m.activeDep]
				return m, updateDependency(m.modules[m.activeMod].Path, target.Name, target.IsDev)
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.errorMsg != "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(fmt.Sprintf(m.localizer.T("error_unknown"), m.errorMsg))
	}

	currentTheme := themes[m.currentThemeIdx].Theme
	focusColor := currentTheme.Focus
	normalColor := currentTheme.Normal
	statusTextColor := currentTheme.Accent

	currentDir := "."
	if len(m.modules) > 0 {
		currentDir = m.modules[m.activeMod].Path
	}
	pkgManager := getPackageManager(currentDir)

	availWidth := max(45, m.width-2)
	availHeight := max(14, m.height-3)

	leftWidth := max(24, availWidth/4)
	rightWidth := availWidth - leftWidth

	rightPanelHeight := max(4, (availHeight / 3))
	statusBoxHeight := 3
	depsPanelHeight := availHeight - rightPanelHeight - statusBoxHeight

	packageStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(normalColor).
		Padding(0, 1).
		Width(leftWidth - 2).
		Height(availHeight - 2)

	tasksStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(normalColor).
		Padding(0, 1).
		Width(rightWidth - 2).
		Height(rightPanelHeight - 2)

	depsStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(normalColor).
		Padding(0, 1).
		Width(rightWidth - 2).
		Height(depsPanelHeight - 2)

	statusBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(normalColor).
		Padding(0, 1).
		Width(rightWidth - 2).
		Height(statusBoxHeight - 2).
		Foreground(statusTextColor)

	bottomBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(normalColor).
		Padding(0, 1).
		Width(availWidth - 2).
		Foreground(statusTextColor).
		Bold(true)

	switch m.activePanel {
	case packagesPanel:
		packageStyle = packageStyle.BorderForeground(focusColor)
	case tasksPanel:
		tasksStyle = tasksStyle.BorderForeground(focusColor)
	case dependenciesPanel:
		depsStyle = depsStyle.BorderForeground(focusColor)
	}

	var pkgContent strings.Builder
	fmt.Fprintf(&pkgContent, "\x1b[1m%s\x1b[0m\n", m.localizer.T("modules_panel_title"))
	for i, mod := range m.modules {
		if i == m.activeMod {
			fmt.Fprintf(&pkgContent, "→ \x1b[1;36m%s\x1b[0m\n", mod.Name)
		} else {
			fmt.Fprintf(&pkgContent, "  \x1b[2m%s\x1b[0m\n", mod.Name)
		}
	}

	var tasksContent strings.Builder
	fmt.Fprintf(&tasksContent, "\x1b[1m%s\x1b[0m\n", fmt.Sprintf(m.localizer.T("scripts_panel_title"), pkgManager))
	if len(m.currentScripts) == 0 {
		tasksContent.WriteString(m.localizer.T("no_scripts_found"))
	} else {
		for i, scriptName := range m.currentScripts {
			cmdText := m.scriptsMap[scriptName]
			if i == m.activeScript && m.activePanel == tasksPanel {
				fmt.Fprintf(&tasksContent, "• \x1b[1;32m%s\x1b[0m \x1b[2m(%s)\x1b[0m\n", scriptName, cmdText)
			} else {
				fmt.Fprintf(&tasksContent, "  %s \x1b[2m(%s)\x1b[0m\n", scriptName, cmdText)
			}
		}
	}

	var depsContent strings.Builder
	fmt.Fprintf(&depsContent, "\x1b[1m%s\x1b[0m\n\n", m.localizer.T("dependencies_panel_title"))

	nameColWidth := max(20, rightWidth-40)

	fmt.Fprintf(&depsContent, "  %-*s %-5s %-10s %s\n\n",
		nameColWidth, m.localizer.T("table_header_name"),
		m.localizer.T("table_header_type"),
		m.localizer.T("table_header_installed_version"),
		m.localizer.T("table_header_latest_version"),
	)

	if len(m.currentDeps) == 0 {
		fmt.Fprintf(&depsContent, "%s", m.localizer.T("no_dependencies_found"))
	} else {
		maxRows := depsPanelHeight - 5
		end := min(m.depScrollStart+maxRows, len(m.currentDeps))

		for i := m.depScrollStart; i < end; i++ {
			dep := m.currentDeps[i]
			isRowSelected := (i == m.activeDep && m.activePanel == dependenciesPanel)

			rowCursor := "  "
			if isRowSelected {
				rowCursor = "→ "
			}

			depType := "prod"
			if dep.IsDev {
				depType = "dev"
			}

			cleanVer := strings.TrimLeft(dep.Version, "^~*")
			hasUpdate := dep.Latest != "fetching..." && dep.Latest != "unknown" && cleanVer != dep.Latest

			displayName := dep.Name
			if len(displayName) > nameColWidth {
				displayName = displayName[:nameColWidth-3] + "..."
			}

			rawName := fmt.Sprintf("%-*s", nameColWidth, displayName)
			rawType := fmt.Sprintf("%-5s", depType)
			rawInstalled := fmt.Sprintf("%-10s", dep.Version)
			rawLatest := fmt.Sprintf("%-10s", dep.Latest)

			var nameStyle, typeStyle, installedStyle, latestStyle string

			if isRowSelected {
				nameStyle = fmt.Sprintf("\x1b[1;7m%s\x1b[0m", rawName)
				typeStyle = fmt.Sprintf("\x1b[7m%s\x1b[0m", rawType)

				if hasUpdate {
					installedStyle = fmt.Sprintf("\x1b[1;31;7m%s\x1b[0m", rawInstalled)
					latestStyle = fmt.Sprintf("\x1b[1;32;7m%s\x1b[0m", rawLatest)
				} else {
					installedStyle = fmt.Sprintf("\x1b[7m%s\x1b[0m", rawInstalled)
					latestStyle = fmt.Sprintf("\x1b[7m%s\x1b[0m", rawLatest)
				}
			} else {
				// Fila normal con colores por tipo (Dev = Cyan, Prod = Green)
				if dep.IsDev {
					nameStyle = fmt.Sprintf("\x1b[36m%s\x1b[0m", rawName)
					typeStyle = fmt.Sprintf("\x1b[36m%s\x1b[0m", rawType)
				} else {
					nameStyle = fmt.Sprintf("\x1b[32m%s\x1b[0m", rawName)
					typeStyle = fmt.Sprintf("\x1b[32m%s\x1b[0m", rawType)
				}

				installedStyle = rawInstalled
				if hasUpdate {
					latestStyle = fmt.Sprintf("\x1b[1;33m%s\x1b[0m", rawLatest)
				} else {
					latestStyle = rawLatest
				}
			}

			fmt.Fprintf(&depsContent, "%s%s %s %s %s\n", rowCursor, nameStyle, typeStyle, installedStyle, latestStyle)
		}
	}

	currentPkgName := "None"
	if len(m.modules) > 0 {
		currentPkgName = m.modules[m.activeMod].Name
	}

	isMonorepoText := m.localizer.T("status_bar_no")
	if len(m.modules) > 1 {
		isMonorepoText = m.localizer.T("status_bar_yes")
	}

	statusText := fmt.Sprintf("@%s | PkgManager: %s | %s: %s",
		currentPkgName,
		pkgManager,
		m.localizer.T("status_bar_monorepo"),
		isMonorepoText,
	)

	rightColumn := lipgloss.JoinVertical(
		lipgloss.Left,
		tasksStyle.Render(tasksContent.String()),
		statusBoxStyle.Render(statusText),
		depsStyle.Render(depsContent.String()),
	)

	mainLayout := lipgloss.JoinHorizontal(
		lipgloss.Top,
		packageStyle.Render(pkgContent.String()),
		rightColumn,
	)

	bottomBar := bottomBoxStyle.Render(m.localizer.T("bottom_bar_shortcuts"))
	baseView := mainLayout + "\n" + bottomBar

	if m.showSettings {
		var settingsContent strings.Builder
		settingsContent.WriteString(lipgloss.NewStyle().Bold(true).Foreground(focusColor).Render(m.localizer.T("settings_title")))
		settingsContent.WriteString("\n\n")

		langLabel := m.localizer.T("settings_lang")
		currentLangCode := m.availableLangs[m.currentLangIdx]
		friendlyLangName := languageNames[currentLangCode]

		if friendlyLangName == "" {
			friendlyLangName = currentLangCode
		}

		if m.settingsCursor == 0 {
			fmt.Fprintf(&settingsContent, "  → \x1b[1;32m%s [%s]\x1b[0m\n", langLabel, friendlyLangName)
		} else {
			fmt.Fprintf(&settingsContent, "    %s [%s]\n", langLabel, friendlyLangName)
		}

		themeLabel := m.localizer.T("settings_theme")
		if m.settingsCursor == 1 {
			fmt.Fprintf(&settingsContent, "  → \x1b[1;32m%s [%s]\x1b[0m\n", themeLabel, themes[m.currentThemeIdx].Name)
		} else {
			fmt.Fprintf(&settingsContent, "    %s [%s]\n", themeLabel, themes[m.currentThemeIdx].Name)
		}

		settingsContent.WriteString("\n\n")
		settingsContent.WriteString(lipgloss.NewStyle().Foreground(normalColor).Render(m.localizer.T("settings_footer")))

		modalBox := lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(focusColor).
			Padding(1, 2).
			Width(50).
			Height(9).
			Align(lipgloss.Left).
			Render(settingsContent.String())

		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			modalBox,
		)
	}

	return baseView
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("%s version %s (commit=%s, date=%s, build=%s)\n", os.Args[0], version, commit, date, buildSource)
		return
	}

	m, cmd := initialModel()
	p := tea.NewProgram(m, tea.WithAltScreen())

	go func() {
		if cmd != nil {
			p.Send(cmd())
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
}
