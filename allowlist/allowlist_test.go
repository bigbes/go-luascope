package allowlist_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bigbes/go-luascope/allowlist"
	"github.com/bigbes/go-luascope/parser"
)

// rules are typical allow-lists for Tarantool migrations.
var rules = allowlist.Rules{
	Modules: []string{
		"checks", "errors", "crud", "crud.*", "log", "json", "msgpack", "uuid",
		"datetime", "decimal", "tt-migrations.helpers",
	},
	Globals: []string{
		"assert", "error", "getmetatable", "ipairs", "next", "pairs", "pcall", "print",
		"rawequal", "rawget", "rawset", "require", "select", "setmetatable", "tonumber",
		"tostring", "type", "unpack", "xpcall",
		"string.*", "table.*", "math.*", "utf8.*", "bit.*",
		"box.space.*", "box.schema.space.create", "box.schema.create_space",
		"box.schema.user.grant", "box.atomic", "box.NULL", "box.tuple.new",
	},
}

func checkWith(t *testing.T, r allowlist.Rules, src string) []string {
	t.Helper()

	chunk, err := parser.Parse([]byte(src), "test")
	require.NoError(t, err)

	checker, err := allowlist.NewChecker(r)
	require.NoError(t, err)

	var out []string
	for _, v := range checker.Check(chunk) {
		out = append(out, v.String())
	}

	return out
}

func check(t *testing.T, src string) []string {
	t.Helper()

	return checkWith(t, rules, src)
}

func TestCheckAllowed(t *testing.T) {
	tests := map[string]string{
		"box":           "box.schema.space.create('users')\nbox.space.users:create_index('pk')",
		"box-dynamic":   "local name = 'users' box.space[name]:replace({1})",
		"box-atomic":    "box.atomic(function() box.space.users:insert(box.tuple.new({1})) end)",
		"modules":       "local log = require('log') local crud = require 'crud'",
		"submodule":     "local utils = require('crud.common.utils')",
		"string-lib":    "local s = string.format('%d', 1) .. ('x'):rep(3)",
		"base":          "for k, v in pairs({}) do print(tostring(k), type(v)) end",
		"local-shadow":  "local debug = {} debug.x = 1 print(debug)",
		"local-require": "local require = function() end require('fiber')",
		"local-box":     "local box = {} box.cfg = 1",
		"self-ref":      "local x = 1 local function f() return f, x end",
		"concat-module": "local m = require('cr' .. 'ud')",
		"paren-module":  "local m = require(('log'))",
		"empty":         "-- nothing here\n",
		"return":        "return function() return box.space.users:count() end",
		"helpers":       "local helpers = require('tt-migrations.helpers')",
		"box-null":      "local x = box.NULL",
		"pcall-require": "local ok, crud = pcall(require, 'crud')",
		"pcall-paren":   "local ok, log = pcall((require), ('l' .. 'og'))",
		"xpcall-require": "local ok, log = xpcall(require, function(e) return e end, " +
			"'log')",
	}

	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, check(t, src))
		})
	}
}

func TestCheckViolations(t *testing.T) {
	const requireValue = "require used as a value; only calls with a constant module name " +
		"are allowed"

	tests := map[string]struct {
		src  string
		want []string
	}{
		"module": {
			"local fiber = require('fiber')",
			[]string{"1:15: module 'fiber' is not allowed"},
		},
		"vshard": {
			"local vshard = require('vshard')",
			[]string{"1:16: module 'vshard' is not allowed"},
		},
		"module-prefix": {
			"local x = require('crudx')",
			[]string{"1:11: module 'crudx' is not allowed"},
		},
		"module-exact": {
			"local u = require('crud.common.utils') local c = require('checks.x')",
			[]string{"1:50: module 'checks.x' is not allowed"},
		},
		"nested-require": {
			"local function f()\n  if x then\n    require('net.box')\n  end\nend",
			[]string{
				"2:6: global variable 'x' is not allowed",
				"3:5: module 'net.box' is not allowed",
			},
		},
		"concat-forbidden": {
			"require('vsh' .. 'ard')",
			[]string{"1:1: module 'vshard' is not allowed"},
		},
		"dynamic-require": {
			"local name = 'fiber' require(name)",
			[]string{"1:22: require with a non-constant module name"},
		},
		"no-args": {
			"require()",
			[]string{"1:1: require with a non-constant module name"},
		},
		"require-value": {
			"local r = require r('fiber')",
			[]string{"1:11: " + requireValue},
		},
		"pcall-forbidden": {
			"local ok, m = pcall(require, 'fiber')",
			[]string{"1:21: module 'fiber' is not allowed"},
		},
		"pcall-dynamic": {
			"local ok, m = pcall(require, name)",
			[]string{
				"1:21: require with a non-constant module name",
				"1:30: global variable 'name' is not allowed",
			},
		},
		"pcall-no-module": {
			"pcall(require)",
			[]string{"1:7: require with a non-constant module name"},
		},
		"xpcall-forbidden": {
			"xpcall(require, debug.traceback, 'fiber')",
			[]string{
				"1:8: module 'fiber' is not allowed",
				"1:17: global variable 'debug.traceback' is not allowed",
			},
		},
		"local-pcall": {
			"local pcall = function(f, ...) return f(...) end pcall(require, 'fiber')",
			[]string{"1:56: " + requireValue},
		},
		"global-read": {
			"print(debug.traceback())",
			[]string{"1:7: global variable 'debug.traceback' is not allowed"},
		},
		"g": {
			"local r = _G.require",
			[]string{"1:11: global variable '_G.require' is not allowed"},
		},
		"loadstring": {
			"loadstring('return 1')()",
			[]string{"1:1: global variable 'loadstring' is not allowed"},
		},
		"global-write": {
			"box = nil",
			[]string{"1:1: assignment to global variable 'box'"},
		},
		"field-write": {
			"box.schema = nil",
			[]string{"1:1: assignment to global variable 'box.schema'"},
		},
		"allowed-field-write": {
			"box.space.users.x = 1",
			[]string{"1:1: assignment to global variable 'box.space.users.x'"},
		},
		"global-function": {
			"function helper() end",
			[]string{"1:10: assignment to global variable 'helper'"},
		},
		"method-definition": {
			"function box.space.users:hook() end",
			[]string{"1:10: assignment to global variable 'box.space.users.hook'"},
		},
		"box-cfg": {
			"box.cfg{read_only = true}",
			[]string{"1:1: global variable 'box.cfg' is not allowed"},
		},
		"box-parent": {
			"local schema = box.schema",
			[]string{"1:16: global variable 'box.schema' is not allowed"},
		},
		"wildcard-not-self": {
			"for name in pairs(box.space) do end",
			[]string{"1:19: global variable 'box.space' is not allowed"},
		},
		"box-dynamic-root": {
			"local x = box[name]",
			[]string{
				"1:11: global variable 'box[?]' is not allowed",
				"1:15: global variable 'name' is not allowed",
			},
		},
		"quoted-key": {
			"local x = box['my key']",
			[]string{"1:11: global variable 'box[\"my key\"]' is not allowed"},
		},
		"multiple": {
			"x, box.space.t = jit.version, os.time()",
			[]string{
				"1:1: assignment to global variable 'x'",
				"1:4: assignment to global variable 'box.space.t'",
				"1:18: global variable 'jit.version' is not allowed",
				"1:31: global variable 'os.time' is not allowed",
			},
		},
		"upvalue-not-global": {
			"local t = {} local function f() t.x = y end",
			[]string{"1:39: global variable 'y' is not allowed"},
		},
		"repeat-scope": {
			"repeat local done = true until done and finished",
			[]string{"1:41: global variable 'finished' is not allowed"},
		},
		"goto-name": {
			"goto = 1",
			[]string{"1:1: assignment to global variable 'goto'"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, check(t, tt.src))
		})
	}
}

// TestPatterns checks how rule entries match global paths.
func TestPatterns(t *testing.T) {
	tests := []struct {
		entry   string
		src     string
		allowed bool
	}{
		{"string", "local s = string", true},
		{"string", "string.format('x')", false},
		{"string.*", "string.format('x')", true},
		{"string.*", "local f = string.x.y", true},
		{"string.*", "local s = string", false},
		{"box.space.*", "box.space[name]:select()", true},
		{"box.space.*.select", "box.space.users:select()", true},
		{"box.space.*.select", "box.space[name]:select()", true},
		{"box.space.*.select", "box.space.users:replace()", false},
		{"box.space.*.select", "local s = box.space.users", false},
		{"box.space.*.select", "box.space.a.b:select()", false},
		{"box.space.*.*", "box.space.users:select()", true},
		{"box.space.*.*", "local s = box.space.users", false},
		{"box.*.users", "local s = box[name].users", true},
		{"*", "local x = anything.at[all]", true},
		{"*.version", "local v = jit.version", true},
		{"*.version", "local v = jit", false},
	}

	for _, tt := range tests {
		r := allowlist.Rules{Globals: []string{tt.entry, "name", "all"}}
		got := checkWith(t, r, tt.src)
		assert.Equal(t, tt.allowed, len(got) == 0, "%q on %q: %v", tt.entry, tt.src, got)
	}
}

// TestModulePatterns checks how rule entries match module names.
func TestModulePatterns(t *testing.T) {
	tests := []struct {
		entry   string
		module  string
		allowed bool
	}{
		{"crud", "crud", true},
		{"crud", "crud.common", false},
		{"crud.*", "crud.common.utils", true},
		{"crud.*", "crud", false},
		{"crud.*.utils", "crud.common.utils", true},
		{"crud.*.utils", "crud.x.y.utils", false},
		{"*.helpers", "tt-migrations.helpers", true},
		{"net.box", "net.box", true},
	}

	for _, tt := range tests {
		r := allowlist.Rules{Modules: []string{tt.entry}, Globals: []string{"require"}}
		got := checkWith(t, r, "require('"+tt.module+"')")
		assert.Equal(t, tt.allowed, len(got) == 0, "%q on %q: %v", tt.entry, tt.module, got)
	}
}

func TestNewCheckerInvalid(t *testing.T) {
	invalid := []string{"", "box..space", ".box", "box.", "bo*x", "box.*x", "create_*", "**"}
	for _, entry := range invalid {
		_, err := allowlist.NewChecker(allowlist.Rules{Globals: []string{entry}})
		require.ErrorIs(t, err, allowlist.ErrInvalidRule, "entry %q", entry)
	}

	_, err := allowlist.NewChecker(allowlist.Rules{Modules: []string{"a..b"}})
	require.ErrorIs(t, err, allowlist.ErrInvalidRule)

	valid := []string{"*", "box.*.space", "box.*"}
	_, err = allowlist.NewChecker(allowlist.Rules{Globals: valid})
	require.NoError(t, err)
}
