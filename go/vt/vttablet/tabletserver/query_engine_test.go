/*
Copyright 2017 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tabletserver

import (
	"expvar"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/youtube/vitess/go/mysql/fakesqldb"
	"github.com/youtube/vitess/go/sqltypes"
	"github.com/youtube/vitess/go/vt/dbconfigs"
	"github.com/youtube/vitess/go/vt/vttablet/tabletserver/planbuilder"
	"github.com/youtube/vitess/go/vt/vttablet/tabletserver/schema"
	"github.com/youtube/vitess/go/vt/vttablet/tabletserver/schema/schematest"
	"github.com/youtube/vitess/go/vt/vttablet/tabletserver/tabletenv"

	querypb "github.com/youtube/vitess/go/vt/proto/query"
)

func TestStrictTransTables(t *testing.T) {
	db := fakesqldb.New(t)
	defer db.Close()
	for query, result := range schematest.Queries() {
		db.AddQuery(query, result)
	}
	testUtils := newTestUtils()
	dbcfgs := testUtils.newDBConfigs(db)

	// Test default behavior.
	config := tabletenv.DefaultQsConfig
	// config.EnforceStrictTransTable is true by default.
	qe := NewQueryEngine(DummyChecker, schema.NewEngine(DummyChecker, config), config)
	qe.InitDBConfig(dbcfgs)
	qe.se.Open()
	if err := qe.Open(); err != nil {
		t.Error(err)
	}
	qe.Close()

	// Check that we fail if STRICT_TRANS_TABLES is not set.
	db.AddQuery(
		"select @@global.sql_mode",
		&sqltypes.Result{
			Fields: []*querypb.Field{{Type: sqltypes.VarChar}},
			Rows:   [][]sqltypes.Value{{sqltypes.NewVarBinary("")}},
		},
	)
	qe = NewQueryEngine(DummyChecker, schema.NewEngine(DummyChecker, config), config)
	qe.InitDBConfig(dbcfgs)
	err := qe.Open()
	wantErr := "require sql_mode to be STRICT_TRANS_TABLES: got ''"
	if err == nil || err.Error() != wantErr {
		t.Errorf("Open: %v, want %s", err, wantErr)
	}
	qe.Close()

	// Test that we succeed if the enforcement flag is off.
	config.EnforceStrictTransTables = false
	qe = NewQueryEngine(DummyChecker, schema.NewEngine(DummyChecker, config), config)
	qe.InitDBConfig(dbcfgs)
	if err := qe.Open(); err != nil {
		t.Fatal(err)
	}
	qe.Close()
}

func TestGetPlanPanicDuetoEmptyQuery(t *testing.T) {
	db := fakesqldb.New(t)
	defer db.Close()
	for query, result := range schematest.Queries() {
		db.AddQuery(query, result)
	}
	testUtils := newTestUtils()
	dbcfgs := testUtils.newDBConfigs(db)
	qe := newTestQueryEngine(10, 10*time.Second, true, dbcfgs)
	qe.se.Open()
	qe.Open()
	defer qe.Close()

	ctx := context.Background()
	logStats := tabletenv.NewLogStats(ctx, "GetPlanStats")
	_, err := qe.GetPlan(ctx, logStats, "", false)
	want := "syntax error"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("qe.GetPlan: %v, want %s", err, want)
	}
}

func TestGetMessageStreamPlan(t *testing.T) {
	db := fakesqldb.New(t)
	defer db.Close()
	for query, result := range schematest.Queries() {
		db.AddQuery(query, result)
	}
	testUtils := newTestUtils()
	dbcfgs := testUtils.newDBConfigs(db)
	qe := newTestQueryEngine(10, 10*time.Second, true, dbcfgs)
	qe.se.Open()
	qe.Open()
	defer qe.Close()

	plan, err := qe.GetMessageStreamPlan("msg")
	if err != nil {
		t.Fatal(err)
	}
	wantPlan := &planbuilder.Plan{
		PlanID: planbuilder.PlanMessageStream,
		Table:  qe.tables["msg"],
	}
	if !reflect.DeepEqual(plan.Plan, wantPlan) {
		t.Errorf("GetMessageStreamPlan(msg): %v, want %v", plan.Plan, wantPlan)
	}
	if plan.Rules == nil || plan.Authorized == nil {
		t.Errorf("GetMessageStreamPlan(msg): Rules or ACLResult are nil. Rules: %v, Authorized: %v", plan.Rules, plan.Authorized)
	}
}

func TestQueryPlanCache(t *testing.T) {
	db := fakesqldb.New(t)
	defer db.Close()
	for query, result := range schematest.Queries() {
		db.AddQuery(query, result)
	}

	firstQuery := "select * from test_table_01"
	secondQuery := "select * from test_table_02"
	db.AddQuery("select * from test_table_01 where 1 != 1", &sqltypes.Result{})
	db.AddQuery("select * from test_table_02 where 1 != 1", &sqltypes.Result{})

	testUtils := newTestUtils()
	dbcfgs := testUtils.newDBConfigs(db)
	qe := newTestQueryEngine(10, 10*time.Second, true, dbcfgs)
	qe.se.Open()
	qe.Open()
	defer qe.Close()

	ctx := context.Background()
	logStats := tabletenv.NewLogStats(ctx, "GetPlanStats")
	qe.SetQueryPlanCacheCap(1)
	firstPlan, err := qe.GetPlan(ctx, logStats, firstQuery, false)
	if err != nil {
		t.Fatal(err)
	}
	if firstPlan == nil {
		t.Fatalf("plan should not be nil")
	}
	secondPlan, err := qe.GetPlan(ctx, logStats, secondQuery, false)
	if err != nil {
		t.Fatal(err)
	}
	if secondPlan == nil {
		t.Fatalf("plan should not be nil")
	}
	expvar.Do(func(kv expvar.KeyValue) {
		_ = kv.Value.String()
	})
	if qe.plans.Size() == 0 {
		t.Fatalf("query plan cache should not be 0")
	}
	qe.ClearQueryPlanCache()
}

func TestNoQueryPlanCache(t *testing.T) {
	db := fakesqldb.New(t)
	defer db.Close()
	for query, result := range schematest.Queries() {
		db.AddQuery(query, result)
	}

	firstQuery := "select * from test_table_01"
	db.AddQuery("select * from test_table_01 where 1 != 1", &sqltypes.Result{})
	db.AddQuery("select * from test_table_02 where 1 != 1", &sqltypes.Result{})

	testUtils := newTestUtils()
	dbcfgs := testUtils.newDBConfigs(db)
	qe := newTestQueryEngine(10, 10*time.Second, true, dbcfgs)
	qe.se.Open()
	qe.Open()
	defer qe.Close()

	ctx := context.Background()
	logStats := tabletenv.NewLogStats(ctx, "GetPlanStats")
	qe.SetQueryPlanCacheCap(1)
	firstPlan, err := qe.GetPlan(ctx, logStats, firstQuery, true)
	if err != nil {
		t.Fatal(err)
	}
	if firstPlan == nil {
		t.Fatalf("plan should not be nil")
	}
	if qe.plans.Size() != 0 {
		t.Fatalf("query plan cache should be 0")
	}
	qe.ClearQueryPlanCache()
}

func TestStatsURL(t *testing.T) {
	db := fakesqldb.New(t)
	defer db.Close()
	for query, result := range schematest.Queries() {
		db.AddQuery(query, result)
	}
	query := "select * from test_table_01"
	db.AddQuery("select * from test_table_01 where 1 != 1", &sqltypes.Result{})
	testUtils := newTestUtils()
	dbcfgs := testUtils.newDBConfigs(db)
	qe := newTestQueryEngine(10, 1*time.Second, true, dbcfgs)
	qe.se.Open()
	qe.Open()
	defer qe.Close()
	// warm up cache
	ctx := context.Background()
	logStats := tabletenv.NewLogStats(ctx, "GetPlanStats")
	qe.GetPlan(ctx, logStats, query, false)

	request, _ := http.NewRequest("GET", "/debug/tablet_plans", nil)
	response := httptest.NewRecorder()
	qe.ServeHTTP(response, request)

	request, _ = http.NewRequest("GET", "/debug/query_stats", nil)
	response = httptest.NewRecorder()
	qe.ServeHTTP(response, request)

	request, _ = http.NewRequest("GET", "/debug/query_rules", nil)
	response = httptest.NewRecorder()
	qe.ServeHTTP(response, request)

	request, _ = http.NewRequest("GET", "/debug/unknown", nil)
	response = httptest.NewRecorder()
	qe.ServeHTTP(response, request)
}

func newTestQueryEngine(queryPlanCacheSize int, idleTimeout time.Duration, strict bool, dbcfgs dbconfigs.DBConfigs) *QueryEngine {
	config := tabletenv.DefaultQsConfig
	config.QueryPlanCacheSize = queryPlanCacheSize
	config.IdleTimeout = float64(idleTimeout) / 1e9
	se := schema.NewEngine(DummyChecker, config)
	qe := NewQueryEngine(DummyChecker, se, config)
	se.InitDBConfig(dbcfgs)
	qe.InitDBConfig(dbcfgs)
	return qe
}
