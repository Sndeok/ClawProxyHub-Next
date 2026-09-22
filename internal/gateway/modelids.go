// modelids.go — 对外模型列表（路由名 ∪ 账号目录模型）。
package gateway

// publicModelIDs 合并路由名与账号目录模型：去重，且保持「路由名在前」的稳定顺序。
// 路由只承担改名 / 映射，删掉路由不应让模型从对外列表消失；
// 受限 key（显式绑定路由）的 DirectModels 为空，因此只会看到被授权的路由名。
func publicModelIDs(routes, direct []string) []string {
	seen := make(map[string]bool, len(routes)+len(direct))
	out := make([]string, 0, len(routes)+len(direct))
	for _, group := range [][]string{routes, direct} {
		for _, id := range group {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
