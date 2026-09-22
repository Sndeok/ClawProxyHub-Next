package router

import (
	"testing"

	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
)

// TestEffectiveStrategy 策略语义：路由单独配置优先，留空跟随全局；
// SetDefaultStrategy 非法值回退内置默认。
func TestEffectiveStrategy(t *testing.T) {
	rt := New(nil)
	if got := rt.DefaultStrategy(); got != model.RouteStrategyDefault {
		t.Fatalf("初始默认策略 = %q，want %q", got, model.RouteStrategyDefault)
	}
	if got := rt.effectiveStrategy(nil); got != model.RouteStrategyDefault {
		t.Fatalf("nil 路由 = %q，want 全局默认 %q", got, model.RouteStrategyDefault)
	}
	if got := rt.effectiveStrategy(&model.Route{}); got != model.RouteStrategyDefault {
		t.Fatalf("空策略路由 = %q，want 全局默认 %q", got, model.RouteStrategyDefault)
	}
	if got := rt.effectiveStrategy(&model.Route{Strategy: model.RouteStrategySticky}); got != model.RouteStrategySticky {
		t.Fatalf("路由单独配置应优先，got %q", got)
	}
	rt.SetDefaultStrategy(model.RouteStrategyRoundRobin)
	if got := rt.effectiveStrategy(&model.Route{}); got != model.RouteStrategyRoundRobin {
		t.Fatalf("全局改轮询后空策略路由 = %q，want round_robin", got)
	}
	if got := rt.effectiveStrategy(&model.Route{Strategy: model.RouteStrategySticky}); got != model.RouteStrategySticky {
		t.Fatalf("全局改动不得覆盖路由单独配置，got %q", got)
	}
	rt.SetDefaultStrategy("bogus")
	if got := rt.DefaultStrategy(); got != model.RouteStrategyDefault {
		t.Fatalf("非法全局策略应回退 %q，got %q", model.RouteStrategyDefault, got)
	}
	rt.SetDefaultStrategy("")
	if got := rt.DefaultStrategy(); got != model.RouteStrategyDefault {
		t.Fatalf("空全局策略应回退 %q，got %q", model.RouteStrategyDefault, got)
	}
}
