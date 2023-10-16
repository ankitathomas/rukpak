package healthchecks

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func AreObjectsKstatusReady(ctx context.Context, client client.Client, objects []client.Object) error {
	var gvkErrors []error

	for _, object := range objects {
		objectKey := types.NamespacedName{
			Name:      object.GetName(),
			Namespace: object.GetNamespace(),
		}

		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(object.GetObjectKind().GroupVersionKind())
		if err := client.Get(ctx, objectKey, u); err != nil {
			gvkErrors = appendResourceError(gvkErrors, object, err.Error())
			continue
		}

		if object.GetObjectKind().GroupVersionKind() == apiregistrationv1.SchemeGroupVersion.WithKind("APIService") {
			obj := &apiregistrationv1.APIService{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, obj); err != nil {
				gvkErrors = appendResourceError(gvkErrors, obj, err.Error())
				continue
			}

			// Check if the APIService is available.
			var isAvailable *apiregistrationv1.APIServiceCondition
			for _, condition := range obj.Status.Conditions {
				if condition.Type == apiregistrationv1.Available {
					isAvailable = &condition
					break
				}
			}
			if isAvailable == nil {
				gvkErrors = appendResourceError(gvkErrors, obj, "Available condition not found")
			} else if isAvailable.Status == apiregistrationv1.ConditionFalse {
				gvkErrors = appendResourceError(gvkErrors, obj, isAvailable.Message)
			}
			continue
		}

		result, err := status.Compute(u)
		if err != nil {
			gvkErrors = appendResourceError(gvkErrors, object, err.Error())
		}

		if result.Status != status.CurrentStatus {
			gvkErrors = appendResourceError(gvkErrors, object, fmt.Sprintf("object %s: %s", result.Status, result.Message))
		}
	}
	return errors.Join(gvkErrors...)
}
