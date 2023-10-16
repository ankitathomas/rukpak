package healthchecks

import (
	"context"
	"fmt"
	"sort"

	helm "helm.sh/helm/v3/pkg/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func AreObjectsReady(ctx context.Context, client client.Client, cfg *rest.Config, objects []client.Object) error {
	var gvkErrors []error
	kCl, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	restCli, err := rest.RESTClientFor(cfg)
	if err != nil {
		return err
	}

	readyChecker := helm.NewReadyChecker(kCl, nil, helm.CheckJobs(true))
	nonReady := map[string][]string{}
	for _, object := range objects {
		// handle APIService separately
		if object.GetObjectKind().GroupVersionKind() == apiregistrationv1.SchemeGroupVersion.WithKind("APIService") {
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

			// Check if the APIService is available.
			obj := &apiregistrationv1.APIService{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, obj); err != nil {
				gvkErrors = appendResourceError(gvkErrors, obj, err.Error())
				continue
			}
			var isAvailable *apiregistrationv1.ConditionStatus
			for _, condition := range obj.Status.Conditions {
				if condition.Type == apiregistrationv1.Available {
					isAvailable = &condition.Status
					break
				}
			}
			if isAvailable == nil || *isAvailable == apiregistrationv1.ConditionFalse {
				gvk := object.GetObjectKind().GroupVersionKind().String()
				if _, ok := nonReady[gvk]; !ok {
					nonReady[gvk] = []string{}
				}
				nonReady[gvk] = append(nonReady[gvk], fmt.Sprintf("%s/%s", object.GetNamespace(), object.GetName()))
			}
			continue
		}

		objInfo := &resource.Info{
			Name:      object.GetName(),
			Namespace: object.GetNamespace(),
			Client:    restCli,
			Object:    object,
		}
		ready, err := readyChecker.IsReady(ctx, objInfo)
		if err != nil {
			gvkErrors = append(gvkErrors, err)
		}
		if !ready {
			gvk := object.GetObjectKind().GroupVersionKind().String()
			if _, ok := nonReady[gvk]; !ok {
				nonReady[gvk] = []string{}
			}
			nonReady[gvk] = append(nonReady[gvk], fmt.Sprintf("%s/%s", object.GetNamespace(), object.GetName()))
		}
	}
	if len(nonReady) > 0 {
		unreadyGvks := []string{}
		for gvk, obj := range nonReady {
			sort.Strings(obj)
			unreadyGvks = append(unreadyGvks, fmt.Sprintf("%s: %s", gvk, obj))
		}
		sort.Strings(unreadyGvks)
		return fmt.Errorf("unhealthy resources: %s", unreadyGvks)
	}
	return nil
}
