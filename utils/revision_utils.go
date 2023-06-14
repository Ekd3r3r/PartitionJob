package utils

import (
	"context"

	apps "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/pkg/controller/history"
	webappv1 "my.domain/partitionJob/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// creates a new controller revision
func CreateNewRevision(partitionJob *webappv1.PartitionJob, revision int64, collisionCount *int32) (*apps.ControllerRevision, error) {
	cr, err := history.NewControllerRevision(partitionJob, partitionJob.GroupVersionKind(), partitionJob.Spec.Selector.MatchLabels, TemplateToRaw(&partitionJob.Spec.Template), revision, collisionCount)

	if err != nil {
		return nil, err
	}

	if cr.ObjectMeta.Annotations == nil {
		cr.ObjectMeta.Annotations = make(map[string]string)
	}

	for key, value := range partitionJob.Annotations {
		cr.ObjectMeta.Annotations[key] = value
	}

	return cr, nil
}

// nextRevision finds the next valid revision number based on revisions. If the length of revisions
// is 0 this is 1. Otherwise, it is 1 greater than the largest revision's Revision. This method
// assumes that revisions has been sorted by Revision.
func GetNextRevision(revisions []*apps.ControllerRevision) int64 {
	count := len(revisions)
	if count <= 0 {
		return 1
	}
	return revisions[count-1].Revision + 1
}

// Removes all controller revision finalizers of PartitionJob resource so that they can be deleted
func RemoveRevisionFinalizers(r client.Client, ctx context.Context, partitionJob *webappv1.PartitionJob) error {
	labelsToMatch := partitionJob.Spec.Selector.MatchLabels
	labelSelector := labels.SelectorFromSet(labelsToMatch)

	revisionList := &apps.ControllerRevisionList{}

	listOptions := &client.ListOptions{Namespace: partitionJob.Namespace, LabelSelector: labelSelector}

	if err := r.List(ctx, revisionList, listOptions); err != nil {
		return err
	}
	finalizerName := "webapp.my.domain.partitionjob/finalizer"
	for index := range revisionList.Items {

		controllerutil.RemoveFinalizer(&revisionList.Items[index], finalizerName)
		if err := r.Update(ctx, &revisionList.Items[index]); err != nil {
			return err
		}
	}

	return nil

}

// returns an array of ControllerRevisions with revisions of PartitionJob resource.
func ListRevisions(r client.Client, ctx context.Context, partitionJob *webappv1.PartitionJob) ([]*apps.ControllerRevision, error) {
	revisionList := &apps.ControllerRevisionList{}
	labelsToMatch := partitionJob.Spec.Selector.MatchLabels
	labelSelector := labels.SelectorFromSet(labelsToMatch)

	listOptions := &client.ListOptions{Namespace: partitionJob.Namespace, LabelSelector: labelSelector}

	if err := r.List(ctx, revisionList, listOptions); err != nil {
		return nil, err
	}

	allRevisions := make([]*apps.ControllerRevision, 0)
	for index := range revisionList.Items {
		allRevisions = append(allRevisions, &revisionList.Items[index])
	}

	return allRevisions, nil
}

func GetAllRevisions(r client.Client, ctx context.Context, partitionJob *webappv1.PartitionJob, revisions []*apps.ControllerRevision) ([]*apps.ControllerRevision, int32, error) {
	var updatedRevision *apps.ControllerRevision
	var collisionCount int32 = 0

	revisionCount := len(revisions)
	history.SortControllerRevisions(revisions)

	updatedRevision, err := CreateNewRevision(partitionJob, GetNextRevision(revisions), &collisionCount)
	if err != nil {
		return nil, collisionCount, err
	}

	//find any equivalent revisions
	equivalentRevisions := history.FindEqualRevisions(revisions, updatedRevision)
	equivalentCount := len(equivalentRevisions)

	if equivalentCount > 0 && history.EqualRevision(revisions[revisionCount-1], equivalentRevisions[equivalentCount-1]) {
		//if the equivalent revision is the last updated revision, no need to do anything else
		return revisions, collisionCount, nil
	} else if equivalentCount > 0 {
		//get equivalent revision and increment the revision
		rev := equivalentRevisions[equivalentCount-1]
		rev.Revision = updatedRevision.Revision

		if err := r.Update(ctx, rev); err != nil {
			return nil, collisionCount, err
		}
	} else {
		//if there is no equivalent revision, we create one
		clone := updatedRevision.DeepCopy()
		clone.Namespace = partitionJob.GetNamespace()

		//only keep the 5 most recent revisions
		diff := revisionCount - 5
		if diff >= 0 {
			for i := 0; i < diff; i++ {
				rev := revisions[i]
				if err := r.Delete(ctx, rev); err != nil {
					return nil, collisionCount, err
				}
			}
		}

		finalizerName := "webapp.my.domain.partitionjob/finalizer"

		if !controllerutil.ContainsFinalizer(clone, finalizerName) {
			controllerutil.AddFinalizer(clone, finalizerName)
		}

		if err := r.Create(ctx, clone); err != nil {
			if errors.IsAlreadyExists(err) {
				var cloneNamespacedName types.NamespacedName = types.NamespacedName{
					Namespace: clone.Namespace,
					Name:      clone.Name,
				}

				if err = r.Get(ctx, cloneNamespacedName, clone); err != nil {
					return nil, collisionCount, err
				}

				collisionCount++
			}

			return nil, collisionCount, err
		}

	}

	updatedRevisions, err := ListRevisions(r, ctx, partitionJob)
	if err != nil {
		return nil, collisionCount, err
	}
	history.SortControllerRevisions(updatedRevisions)

	return updatedRevisions, collisionCount, nil
}
