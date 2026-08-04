// SchemeBuilder registers the dagmar CRD types into a runtime scheme. Uses the controller-runtime
// scheme.Builder (Kubebuilder idiom) whose Register takes the type objects directly.
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is the dagmar API group + version.
	GroupVersion = schema.GroupVersion{Group: "dagmar.denkhaus.io", Version: "v1alpha1"}

	// SchemeBuilder registers the CRD types (Project, Run + their Lists).
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the dagmar types to a scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
