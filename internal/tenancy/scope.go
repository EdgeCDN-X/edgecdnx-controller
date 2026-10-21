package tenancy

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation"
)

type Scope struct {
	Namespaced      bool
	TargetNamespace string
}

func (s Scope) Validate() error {
	if !s.Namespaced {
		if problems := validation.IsDNS1123Label(s.TargetNamespace); len(problems) != 0 {
			return fmt.Errorf("invalid target namespace %q: %v", s.TargetNamespace, problems)
		}
	}
	return nil
}

func (s Scope) ValidateNamespace(namespace string) error {
	if !s.Namespaced && namespace != s.TargetNamespace {
		return fmt.Errorf("namespace must be %q", s.TargetNamespace)
	}
	return nil
}

func (s Scope) ValidateProject(namespace, name string) error {
	if err := s.ValidateNamespace(namespace); err != nil {
		return err
	}
	if s.Namespaced && name != namespace {
		return fmt.Errorf("project name must match namespace %q", namespace)
	}
	return nil
}
