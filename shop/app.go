// Package shop adds an independent "cloud drive shop" on top of StorFlow:
// sellers open a storefront, list folders for sale, get paid through an epay
// gateway, and buyers keep receiving updates of the folders they bought.
//
// The module is deliberately self contained:
//
//   - its tables are prefixed with crshop_ and created by package store;
//   - it never edits the ent schema;
//   - it only depends on the host through the public dependency interface, the
//     file manager and the share client;
//   - the single hook into the core file manager is pkg/sharegate, which keeps
//     the paywall of paid shares testable and optional.
package shop

import (
	"context"
	stdsql "database/sql"
	"sync"
	"time"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/pkg/fileguard"
	"github.com/Mxucc/StorFlow/pkg/sharegate"
	"github.com/Mxucc/StorFlow/pkg/shoplink"
	"github.com/Mxucc/StorFlow/shop/service"
	"github.com/Mxucc/StorFlow/shop/store"
)

var (
	mu      sync.Mutex
	app     *App
	initErr error
)

// App is the shop module instance.
type App struct {
	dep   dependency.Dep
	store *store.Store
	svc   *service.Service
}

// Init wires the module: it creates the module tables, loads the settings and
// registers the share paywall. Calling it twice returns the same instance.
//
// The function is safe to call before the host database is reachable: any
// failure is remembered and reported to the caller.
func Init(dep dependency.Dep) (*App, error) {
	mu.Lock()
	defer mu.Unlock()

	if app != nil {
		return app, initErr
	}

	handle, dialect := dep.RawDB()
	if handle == nil {
		initErr = errNoDatabase
		return nil, initErr
	}

	st := store.New(handle, storeDialect(dialect), dep.Logger())
	if err := st.Migrate(context.Background()); err != nil {
		initErr = err
		return nil, initErr
	}

	svc := service.New(dep, st)
	if err := svc.Reload(context.Background()); err != nil {
		initErr = err
		return nil, initErr
	}

	// The core file manager asks this gate before exposing a paid share.
	sharegate.Register(svc.CheckShareAccess)
	// The registration flow reports invitees through this hook.
	shoplink.Register(svc.BindReferralFromRegister)
	// Files that were sold must not disappear from under the buyers.
	fileguard.Register(svc.GuardDeletion)

	app = &App{dep: dep, store: st, svc: svc}
	go app.janitor()
	return app, nil
}

// janitor closes stale pending orders and refreshes the cached settings. It is
// owned by the module so that no host cron entry has to be added.
func (a *App) janitor() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	ctx := context.Background()
	for range ticker.C {
		now := time.Now().Unix()
		closed, err := a.store.CloseExpiredOrders(ctx, now, 200)
		if err != nil {
			a.dep.Logger().Warning("Shop: failed to close expired orders: %s", err)
		} else if closed > 0 {
			a.dep.Logger().Debug("Shop: closed %d expired orders.", closed)
		}

		if err := a.svc.Reload(ctx); err != nil {
			a.dep.Logger().Warning("Shop: failed to reload settings: %s", err)
		}

		// Drop the storefront images no shop or product references any more.
		if removed, err := a.svc.PurgeAssets(ctx, 50); err != nil {
			a.dep.Logger().Warning("Shop: image cleanup failed: %s", err)
		} else if removed > 0 {
			a.dep.Logger().Debug("Shop: removed %d unused image(s).", removed)
		}

		// Settle orders whose payment notification was lost.
		if settled, err := a.svc.ReconcilePendingOrders(ctx, 20, 180); err != nil {
			a.dep.Logger().Warning("Shop: gateway reconciliation failed: %s", err)
		} else if settled > 0 {
			a.dep.Logger().Info("Shop: settled %d order(s) from the payment gateway.", settled)
		}

		// Release the income of platform orders whose unfreeze window has passed:
		// a seller may only withdraw money that matured.
		if released, err := a.svc.ReleaseMaturedSales(ctx, 200); err != nil {
			a.dep.Logger().Warning("Shop: failed to release matured sales: %s", err)
		} else if released > 0 {
			a.dep.Logger().Info("Shop: released the income of %d matured order(s).", released)
		}

		// The ledger is the source of truth: report (never silently "fix")
		// any wallet that drifted from it.
		if drifted, err := a.svc.Reconcile(ctx); err != nil {
			a.dep.Logger().Warning("Shop: reconciliation failed: %s", err)
		} else if drifted > 0 {
			a.dep.Logger().Error("Shop: %d shop wallet(s) drifted from the ledger, please review.", drifted)
		}
	}
}

// Current returns the initialized module, or nil when Init was never called.
func Current() *App {
	mu.Lock()
	defer mu.Unlock()
	return app
}

// Service returns the business service of the module.
func (a *App) Service() *service.Service { return a.svc }

// Store returns the persistence layer of the module.
func (a *App) Store() *store.Store { return a.store }

// Service returns the initialized service, or nil.
func Service() *service.Service {
	if a := Current(); a != nil {
		return a.svc
	}
	return nil
}

func storeDialect(name string) store.Dialect {
	switch name {
	case string(store.DialectMySQL), "mariadb":
		return store.DialectMySQL
	case string(store.DialectPostgres):
		return store.DialectPostgres
	default:
		return store.DialectSQLite
	}
}

var errNoDatabase = stdsql.ErrConnDone
