package assembly

import auth "gochen/auth"

// IModuleAuthzProvider 暴露模块声明式 authz 目录。
type IModuleAuthzProvider interface {
	AuthzRegistration() auth.ModuleRegistration
}
