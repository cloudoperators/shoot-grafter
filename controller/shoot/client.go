package shoot

import (
	"context"
	"time"

	gardenerAuthenticationv1alpha1 "github.com/gardener/gardener/pkg/apis/authentication/v1alpha1"
	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getShootClusterClient(ctx context.Context, gardenClient client.Client, shoot *gardenerv1beta1.Shoot) (client.Client, error) {
	expiration := 10 * time.Minute
	expirationSeconds := int64(expiration.Seconds())
	adminKubeconfigRequest := &gardenerAuthenticationv1alpha1.AdminKubeconfigRequest{
		Spec: gardenerAuthenticationv1alpha1.AdminKubeconfigRequestSpec{
			ExpirationSeconds: &expirationSeconds,
		},
	}
	err := gardenClient.SubResource("adminkubeconfig").Create(ctx, shoot, adminKubeconfigRequest)
	if err != nil {
		return nil, err
	}
	kubeCfg := adminKubeconfigRequest.Status.Kubeconfig

	clientCfg, err := clientcmd.NewClientConfigFromBytes(kubeCfg)
	if err != nil {
		return nil, err
	}

	restCfg, err := clientCfg.ClientConfig()
	if err != nil {
		return nil, err
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, err
	}

	shootClient, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}

	return shootClient, nil
}
