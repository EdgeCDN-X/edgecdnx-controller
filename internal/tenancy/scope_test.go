package tenancy

import "testing"

func TestScope(t *testing.T) {
	for _, test := range []struct {
		name      string
		scope     Scope
		namespace string
		project   string
		allowed   bool
	}{
		{"dedicated namespace", Scope{Namespaced: true}, "tenant", "tenant", true},
		{"wrong project name", Scope{Namespaced: true}, "tenant", "other", false},
		{"shared namespace", Scope{TargetNamespace: "edgecdnx"}, "edgecdnx", "tenant", true},
		{"outside shared namespace", Scope{TargetNamespace: "edgecdnx"}, "other", "tenant", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.scope.ValidateProject(test.namespace, test.project); (err == nil) != test.allowed {
				t.Fatalf("ValidateProject() = %v, allowed = %v", err, test.allowed)
			}
		})
	}
	if err := (Scope{}).Validate(); err == nil {
		t.Fatal("empty shared namespace must be rejected")
	}
}

func TestCacheScope(t *testing.T) {
	if objects := (Scope{Namespaced: true}).CacheObjects(); objects != nil {
		t.Fatal("dedicated placement must watch all tenant namespaces")
	}
	objects := (Scope{TargetNamespace: "edgecdnx"}).CacheObjects()
	if len(objects) != 8 {
		t.Fatalf("expected all eight tenant CRDs, got %d", len(objects))
	}
	for object, options := range objects {
		if len(options.Namespaces) != 1 {
			t.Fatalf("%T must watch exactly one namespace", object)
		}
		if _, ok := options.Namespaces["edgecdnx"]; !ok {
			t.Fatalf("%T does not watch the target namespace", object)
		}
	}
}
