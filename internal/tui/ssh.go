package tui

import (
	"os"

	"github.com/fluffynuts/quickemu-tui/internal/config"
)

// sshUserDefault is the user pre-filled in the SSH prompt: the one last used,
// else the current user.
func (m *Model) sshUserDefault() string {
	if m.opts.SSHUser != "" {
		return m.opts.SSHUser
	}
	return os.Getenv("USER")
}

// rememberSSHUser keeps user as the next prompt's default, across restarts too
// when there is a config file to save into. A save failure is flashed; the
// connection still goes ahead.
func (m *Model) rememberSSHUser(user string) {
	m.opts.SSHUser = user
	if m.opts.ConfigPath == "" {
		return
	}
	if err := config.Update(m.opts.ConfigPath, func(c *config.Config) { c.SSHUser = user }); err != nil {
		m.setFlash("couldn't remember ssh user: "+err.Error(), true)
	}
}
