package gc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLibraryHardDeleteLeasePinsGlobalSerial(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("library hard-delete lease no longer pins global SERIAL: runtime.Caller failed")
	}
	sourcePath := filepath.Join(filepath.Dir(testFile), "store_cassandra.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, sourcePath, nil, 0)
	if err != nil {
		t.Fatalf("library hard-delete lease no longer pins global SERIAL: parse %s: %v", sourcePath, err)
	}

	for _, operation := range []struct {
		name       string
		helperName string
		lwtCount   int
	}{
		{name: "AcquireLibraryHardDeleteLockLease", helperName: "acquireHardDeleteLock", lwtCount: 2},
		{name: "RenewLibraryHardDeleteLockLease", helperName: "renewHardDeleteLock", lwtCount: 1},
		{name: "ReleaseLibraryHardDeleteLockLease", helperName: "releaseHardDeleteLock", lwtCount: 1},
	} {
		decl := hardDeleteLeaseFuncDecl(file, operation.name)
		pinnedCall := false
		ast.Inspect(decl.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee, ok := call.Fun.(*ast.Ident)
			if !ok || callee.Name != operation.helperName || len(call.Args) != 6 {
				return true
			}
			serialDomain, ok := call.Args[5].(*ast.Ident)
			pinnedCall = ok && serialDomain.Name == "hardDeleteLockGlobalSerialDomain"
			return true
		})
		if !pinnedCall {
			t.Errorf("library hard-delete lease no longer pins global SERIAL: %s must pass hardDeleteLockGlobalSerialDomain to %s", operation.name, operation.helperName)
		}

		helper := hardDeleteLeaseFuncDecl(file, operation.helperName)
		lwtCalls := 0
		ast.Inspect(helper.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.Ident)
			if ok && selector.Name == "applyHardDeleteLockSerialDomain" {
				lwtCalls++
			}
			return true
		})
		if lwtCalls != operation.lwtCount {
			t.Errorf("library hard-delete lease no longer pins global SERIAL: %s applies its serial domain to %d LWTs, want %d", operation.name, lwtCalls, operation.lwtCount)
		}
	}

	serialDomain := hardDeleteLeaseFuncDecl(file, "applyHardDeleteLockSerialDomain")
	pinsGlobalSerial := false
	ast.Inspect(serialDomain.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		method, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || method.Sel.Name != "SerialConsistency" || len(call.Args) != 1 {
			return true
		}
		level, ok := call.Args[0].(*ast.SelectorExpr)
		if !ok || level.Sel.Name != "Serial" {
			return true
		}
		pkg, ok := level.X.(*ast.Ident)
		pinsGlobalSerial = ok && pkg.Name == "gocql"
		return true
	})
	if !pinsGlobalSerial {
		t.Fatal("library hard-delete lease no longer pins global SERIAL: query domain must call SerialConsistency(gocql.Serial)")
	}
}

func hardDeleteLeaseFuncDecl(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return fn
		}
	}
	panic("library hard-delete lease no longer pins global SERIAL: missing function " + name)
}
