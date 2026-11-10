// Code equivalent to controller-gen output (object:generate). Keep in sync
// with webapp_types.go, or regenerate with `controller-gen object paths=./api/...`.

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto copies the receiver into out.
func (in *WebAppSpec) DeepCopyInto(out *WebAppSpec) {
	*out = *in
	if in.Replicas != nil {
		out.Replicas = new(int32)
		*out.Replicas = *in.Replicas
	}
	if in.Resources != nil {
		out.Resources = new(corev1.ResourceRequirements)
		in.Resources.DeepCopyInto(out.Resources)
	}
}

// DeepCopy creates a new WebAppSpec.
func (in *WebAppSpec) DeepCopy() *WebAppSpec {
	if in == nil {
		return nil
	}
	out := new(WebAppSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the receiver into out.
func (in *WebAppStatus) DeepCopyInto(out *WebAppStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}

// DeepCopy creates a new WebAppStatus.
func (in *WebAppStatus) DeepCopy() *WebAppStatus {
	if in == nil {
		return nil
	}
	out := new(WebAppStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the receiver into out.
func (in *WebApp) DeepCopyInto(out *WebApp) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy creates a new WebApp.
func (in *WebApp) DeepCopy() *WebApp {
	if in == nil {
		return nil
	}
	out := new(WebApp)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *WebApp) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto copies the receiver into out.
func (in *WebAppList) DeepCopyInto(out *WebAppList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]WebApp, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy creates a new WebAppList.
func (in *WebAppList) DeepCopy() *WebAppList {
	if in == nil {
		return nil
	}
	out := new(WebAppList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *WebAppList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
