package surge

import "github.com/backrunner/xcore-bridge/internal/vless"

type InstallOptions struct {
	Nodes     []vless.Node
	Names     []string
	ExecPath  string
	BasePort  int
	WriteFile bool

	portAvailable func(string, int) bool
}

type RemoveOptions struct {
	Names     []string
	WriteFile bool
}

type RenameOptions struct {
	From      string
	To        string
	WriteFile bool
}

type ReplaceOptions struct {
	Name      string
	Node      vless.Node
	ExecPath  string
	WriteFile bool
}

type InstallResult struct {
	Profile     string
	PolicyNames []string
	LocalPorts  []int
	BackupPath  string
}

type RemoveResult struct {
	Profile      string
	RemovedNames []string
	BackupPath   string
}

type RenameResult struct {
	Profile    string
	OldName    string
	NewName    string
	BackupPath string
}

type ReplaceResult struct {
	Profile    string
	PolicyName string
	LocalPort  int
	BackupPath string
}

type ManagedPolicy struct {
	Name      string
	Link      string
	LocalHost string
	LocalPort int
	RunHost   string
	RunPort   int
}

type ProfileCandidate struct {
	Path   string
	Source string
}
