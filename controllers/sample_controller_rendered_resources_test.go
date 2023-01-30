package controllers_test

import (
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/kubernetes"

	"github.com/kyma-project/template-operator/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	sampleName = "kyma-sample"
	podNs      = "redis"
	podName    = "busybox-pod"
)

func testFn(sampleCR *v1alpha1.Sample, desiredState v1alpha1.State, desiredConditionStatus metav1.ConditionStatus,
	resourceCheck func(g Gomega) bool,
) {
	// create SampleCR
	Expect(k8sClient.Create(ctx, sampleCR)).To(Succeed())

	// check if SampleCR is in the desired State
	sampleCRKey := client.ObjectKeyFromObject(sampleCR)
	Eventually(getCRStatus(sampleCRKey)).
		WithTimeout(30 * time.Second).
		WithPolling(500 * time.Millisecond).
		Should(Equal(CRStatus{State: desiredState, InstallConditionStatus: desiredConditionStatus, Err: nil}))

	// check if deployed resources are up and running
	Eventually(resourceCheck).
		WithTimeout(30 * time.Second).
		WithPolling(500 * time.Millisecond).
		Should(BeTrue())

	// clean up SampleCR
	Expect(k8sClient.Delete(ctx, sampleCR)).To(Succeed())

	// check installed resources are deleted
	Eventually(checkDeleted(sampleCRKey)).
		WithTimeout(30 * time.Second).
		WithPolling(500 * time.Millisecond).
		Should(BeTrue())
}

func createSampleCR(sampleName string, path string) *v1alpha1.Sample {
	return &v1alpha1.Sample{
		TypeMeta: metav1.TypeMeta{
			Kind:       string(v1alpha1.SampleKind),
			APIVersion: v1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      sampleName,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.SampleSpec{ResourceFilePath: path},
	}
}

func getPod(namespace, podName string) func(g Gomega) bool {
	return func(g Gomega) bool {
		clientSet, err := kubernetes.NewForConfig(reconciler.Config)
		g.Expect(err).ToNot(HaveOccurred())

		pod, err := clientSet.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return false
		}

		// Because there are no controllers monitoring built-in resources, state of objects do not get updated.
		// Thus, 'Ready'-State of pod needs to be set manually.
		pod.Status.Conditions = append(pod.Status.Conditions, v1.PodCondition{
			Type:   v1.PodReady,
			Status: v1.ConditionTrue,
		})

		_, err = clientSet.CoreV1().Pods(namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
		g.Expect(err).ToNot(HaveOccurred())
		return true
	}
}

type CRStatus struct {
	State                  v1alpha1.State
	InstallConditionStatus metav1.ConditionStatus
	Err                    error
}

func getCRStatus(sampleObjKey client.ObjectKey) func(g Gomega) CRStatus {
	return func(g Gomega) CRStatus {
		sampleCR := &v1alpha1.Sample{}
		err := k8sClient.Get(ctx, sampleObjKey, sampleCR)
		if err != nil {
			return CRStatus{State: v1alpha1.StateError, Err: err}
		}
		g.Expect(err).NotTo(HaveOccurred())
		condition := meta.FindStatusCondition(sampleCR.Status.Conditions, v1alpha1.ConditionTypeInstallation)
		g.Expect(condition).ShouldNot(BeNil())
		return CRStatus{
			State:                  sampleCR.Status.State,
			InstallConditionStatus: condition.Status,
			Err:                    nil,
		}
	}
}

func checkDeleted(sampleObjKey client.ObjectKey) func(g Gomega) bool {
	return func(g Gomega) bool {
		clientSet, err := kubernetes.NewForConfig(reconciler.Config)
		g.Expect(err).ToNot(HaveOccurred())

		// check if Pod resource is deleted
		_, err = clientSet.CoreV1().Pods(podNs).Get(ctx, podName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			sampleCR := v1alpha1.Sample{}
			// check if reconciled resource is also deleted
			err = k8sClient.Get(ctx, sampleObjKey, &sampleCR)
			return errors.IsNotFound(err)
		}
		return false
	}
}

var _ = Describe("Sample CR scenarios", Ordered, func() {
	DescribeTable("should set SampleCR to appropriate states",
		testFn,
		Entry("when Sample CR is created with the correct resource path",
			createSampleCR(sampleName, "./test/busybox/manifest"),
			v1alpha1.StateReady,
			metav1.ConditionTrue,
			getPod(podNs, podName),
		),
		Entry("when Sample CR is created with an incorrect resource path",
			createSampleCR(sampleName, "invalid/path"),
			v1alpha1.StateError,
			metav1.ConditionFalse,
			func(g Gomega) bool { return true },
		),
	)
})
