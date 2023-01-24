package controllers_test

import (
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/kubernetes"

	"github.com/kyma-project/template-operator/api/v1alpha1"
	"github.com/kyma-project/template-operator/controllers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	helmPodName = "busybox-helm-pod"
)

func testHelmFn(sampleCR *v1alpha1.SampleHelm, desiredState v1alpha1.State, desiredConditionStatus metav1.ConditionStatus,
	resourceCheck func(g Gomega) bool) {
	// create SampleCR
	Expect(k8sClient.Create(ctx, sampleCR)).To(Succeed())

	// check if SampleCR is in the desired State
	sampleCRKey := client.ObjectKeyFromObject(sampleCR)
	Eventually(getHelmCRStatus(sampleCRKey)).
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
	Eventually(checkHelmCRDeleted(sampleCRKey)).
		WithTimeout(30 * time.Second).
		WithPolling(500 * time.Millisecond).
		Should(BeTrue())
}

func createSampleHelmCR(sampleName string, path string) *v1alpha1.SampleHelm {
	return &v1alpha1.SampleHelm{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sampleName,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.SampleHelmSpec{ChartPath: path},
	}
}

func getHelmCRStatus(sampleObjKey client.ObjectKey) func(g Gomega) CRStatus {
	return func(g Gomega) CRStatus {
		sampleCR := &v1alpha1.SampleHelm{}
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

func checkHelmCRDeleted(sampleObjKey client.ObjectKey) func(g Gomega) bool {
	return func(g Gomega) bool {
		clientSet, err := kubernetes.NewForConfig(reconciler.Config)
		g.Expect(err).ToNot(HaveOccurred())

		// check if Pod resource is deleted
		_, err = clientSet.CoreV1().Pods(podNs).Get(ctx, podName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			sampleCR := v1alpha1.SampleHelm{}
			// check if reconciled resource is also deleted
			err = k8sClient.Get(ctx, sampleObjKey, &sampleCR)
			return errors.IsNotFound(err)
		}
		return false
	}
}

var _ = Describe("Sample Helm CR scenarios", Ordered, func() {
	DescribeTable("should set SampleHelmCR to appropriate states",
		testHelmFn,
		Entry("when SampleHelmCR is created with the correct resource path",
			createSampleHelmCR(sampleName, "./test/busybox"),
			v1alpha1.StateReady,
			metav1.ConditionTrue,
			getPod(controllers.CustomNs, helmPodName),
		),
		Entry("when SampleHelmCR is created with an incorrect resource path",
			createSampleHelmCR(sampleName, "invalid/path"),
			v1alpha1.StateError,
			metav1.ConditionFalse,
			func(g Gomega) bool { return true },
		),
	)
})
