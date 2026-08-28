package waiters

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"github.com/selectel/dbaas-go"
	dbaas_v2 "github.com/selectel/dbaas-go/v2"
)

func WaitForDBaaSDatastoreV1ActiveState(
	ctx context.Context, client *dbaas.API, datastoreID string, timeout time.Duration,
) error {
	pending := []string{
		string(dbaas.StatusPendingCreate),
		string(dbaas.StatusPendingUpdate),
		string(dbaas.StatusResizing),
	}
	target := []string{
		string(dbaas.StatusActive),
	}

	stateConf := &resource.StateChangeConf{
		Pending:    pending,
		Target:     target,
		Refresh:    dbaasDatastoreV1StateRefreshFunc(ctx, client, datastoreID),
		Timeout:    timeout,
		Delay:      10 * time.Second,
		MinTimeout: 20 * time.Second,
	}

	_, err := stateConf.WaitForState()
	if err != nil {
		return fmt.Errorf(
			"error waiting for the datastore %s to become 'ACTIVE': %s",
			datastoreID, err)
	}

	return nil
}

func DBaaSDatastoreV1DeleteStateRefreshFunc(ctx context.Context, client *dbaas.API, datastoreID string) resource.StateRefreshFunc {
	return func() (any, string, error) {
		d, err := client.Datastore(ctx, datastoreID)
		if err != nil {
			var dbaasError *dbaas.DBaaSAPIError
			if errors.As(err, &dbaasError) {
				return d, strconv.Itoa(dbaasError.StatusCode()), nil
			}

			return nil, "", err
		}

		return d, strconv.Itoa(http.StatusOK), err
	}
}

func dbaasDatastoreV1StateRefreshFunc(ctx context.Context, client *dbaas.API, datastoreID string) resource.StateRefreshFunc {
	return func() (any, string, error) {
		d, err := client.Datastore(ctx, datastoreID)
		if err != nil {
			return nil, "", err
		}

		return d, string(d.Status), nil
	}
}

func WaitForDBaaSDatastoreV2RunningActive(
	ctx context.Context, client *dbaas_v2.API, datastoreID string, timeout time.Duration,
) error {
	pending := []string{
		"UNKNOWN/CREATING",
		"RUNNING/UPDATING",
		"RUNNING/RESIZING",
	}
	target := []string{
		"RUNNING/ACTIVE",
	}

	stateConf := &retry.StateChangeConf{
		Pending:    pending,
		Target:     target,
		Refresh:    dbaasDatastoreV2StateRefreshFunc(ctx, client, datastoreID),
		Timeout:    timeout,
		Delay:      10 * time.Second,
		MinTimeout: 20 * time.Second,
	}

	_, err := stateConf.WaitForStateContext(ctx)
	if err != nil {
		return fmt.Errorf(
			"error waiting for the datastore %s to become 'RUNNING/ACTIVE': %s",
			datastoreID, err)
	}

	return nil
}

func dbaasDatastoreV2StateRefreshFunc(ctx context.Context, client *dbaas_v2.API, datastoreID string) retry.StateRefreshFunc {
	// Need to get engine or instead clien get interface with Get method
	return func() (any, string, error) {
		d, err := client.ClickHouse.GetDatastore(ctx, datastoreID)
		if err != nil {
			return nil, "", err
		}

		combinedStatus := fmt.Sprintf("%s/%s", d.State, d.Status)

		return d, combinedStatus, nil
	}
}

func WaitForDBaaSDatastoreV2Deleted(
	ctx context.Context, client *dbaas_v2.API, datastoreID string, timeout time.Duration,
) error {

	stateConf := &retry.StateChangeConf{
		Pending:    []string{strconv.Itoa(http.StatusOK)},
		Target:     []string{strconv.Itoa(http.StatusNotFound)},
		Refresh:    DBaaSDatastoreV2DeleteStateRefreshFunc(ctx, client, datastoreID),
		Timeout:    timeout,
		Delay:      10 * time.Second,
		MinTimeout: 15 * time.Second,
	}

	_, err := stateConf.WaitForStateContext(ctx)
	if err != nil {
		return fmt.Errorf(
			"error waiting for the datastore %s to become deleted': %s",
			datastoreID, err)
	}

	return nil
}
func DBaaSDatastoreV2DeleteStateRefreshFunc(ctx context.Context, client *dbaas_v2.API, datastoreID string) retry.StateRefreshFunc {
	return func() (any, string, error) {
		d, err := client.ClickHouse.GetDatastore(ctx, datastoreID)
		if err != nil {
			var dbaasError *dbaas_v2.DBaaSAPIError
			if errors.As(err, &dbaasError) {
				return d, strconv.Itoa(dbaasError.StatusCode()), nil
			}

			return nil, "", err
		}

		return d, strconv.Itoa(http.StatusOK), err
	}
}
