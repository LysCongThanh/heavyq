package config

import (
	"errors"
	"fmt"
	"strings"
)

// normalizeCommon fills the defaults the local machine and remotes share and
// rejects values out of range.
func (m *MachineConfig) normalizeCommon() error {
	if m.HeavySlots < 0 || m.LightSlots < 0 || m.HeavySlots+m.LightSlots > 64 {
		return errors.New("heavy_slots/light_slots must be >= 0 and total <= 64")
	}
	if m.HeavySlots+m.LightSlots == 0 {
		return errors.New("needs at least one slot")
	}
	if m.MaxLoadRatio == 0 {
		m.MaxLoadRatio = defaultLoadRatio
	}
	if m.MaxLoadRatio < 0 || m.MaxLoadRatio > 64 {
		return errors.New("max_load_ratio must be in (0, 64]")
	}
	for _, v := range []*int{&m.JobMB, &m.LightMB, &m.ReserveMB} {
		if *v < 0 {
			return errors.New("job_mb/light_mb/reserve_mb must be >= 0")
		}
	}
	if m.JobMB == 0 {
		m.JobMB = defaultJobMB
	}
	if m.LightMB == 0 {
		m.LightMB = defaultLightMB
	}
	if m.ReserveMB == 0 {
		m.ReserveMB = defaultReserveMB
	}
	return nil
}

// normalizeRemote fills the defaults of a [[remote]] and refuses anything
// unsafe to put on a command line or in a remote path.
func (m *MachineConfig) normalizeRemote() error {
	if m.HeavySlots == 0 && m.LightSlots == 0 {
		m.HeavySlots = 2
	}
	if err := m.normalizeCommon(); err != nil {
		return err
	}
	if len(m.LegacyLockFiles) > 0 {
		return errors.New("legacy_lock_files is only valid in [local]")
	}
	if m.WorkDir == "" {
		m.WorkDir = defaultRemoteWorkDir
	}
	if strings.ContainsAny(m.WorkDir, "'\"`$\\\n ") || strings.Contains(m.WorkDir, "..") {
		return fmt.Errorf("work_dir %q contains unsafe characters", m.WorkDir)
	}
	if err := validateEnvPath(m.EnvPath); err != nil {
		return err
	}
	if len(m.Transport) == 0 {
		if m.SSH == "" {
			return errors.New("set ssh (an ssh alias) or transport")
		}
		if !sshAliasPattern.MatchString(m.SSH) {
			return fmt.Errorf("ssh %q must be a plain host alias (put options in ~/.ssh/config)", m.SSH)
		}
		m.Transport = []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", m.SSH}
	}
	if m.GitURL == "" {
		if m.SSH == "" {
			return errors.New("git_url is required when transport is customised without ssh")
		}
		m.GitURL = m.SSH + ":" + strings.TrimSuffix(m.WorkDir, "/") + "/git"
	}
	return nil
}
