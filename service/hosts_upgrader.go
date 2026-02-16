package service

type HostsUpgrader struct {
	moduleName  string
	upgradeFunc func(string, []string)
}

func NewHostsUpgrader(
	moduleName string,
	upgradeFunc func(string, []string),
) HostsUpgrader {
	return HostsUpgrader{
		moduleName:  moduleName,
		upgradeFunc: upgradeFunc,
	}
}

func (s HostsUpgrader) Upgrade(hosts []string) {
	s.upgradeFunc(s.moduleName, hosts)
}
