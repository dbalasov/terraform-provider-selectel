package selectel

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/selectel/dbaas-go"
	dbaas_v2 "github.com/selectel/dbaas-go/v2"
	dbaas_v2_ch "github.com/selectel/dbaas-go/v2/clickhouse"
	dbaas_v2_common "github.com/selectel/dbaas-go/v2/common"
	waiters "github.com/terraform-providers/terraform-provider-selectel/selectel/waiters/dbaas"
)

const clickhouseDatastoreType = "clickhouse"

func getDBaaSV2Client(d *schema.ResourceData, meta any) (*dbaas_v2.API, diag.Diagnostics) {
	config := meta.(*Config)
	projectID := d.Get("project_id").(string)
	region := d.Get("region").(string)

	selvpcClient, err := config.GetSelVPCClientWithProjectScope(projectID)
	if err != nil {
		return nil, diag.FromErr(fmt.Errorf("can't get project-scope selvpc client for dbaas: %w", err))
	}

	err = validateRegion(selvpcClient, DBaaSv2, region)
	if err != nil {
		return nil, diag.FromErr(fmt.Errorf("can't validate region: %w", err))
	}

	endpoint, err := selvpcClient.Catalog.GetEndpoint(DBaaSv2, region)
	if err != nil {
		return nil, diag.FromErr(fmt.Errorf("can't get endpoint to init dbaas v2 client: %w", err))
	}

	client, err := dbaas.NewDBAASClientV2(selvpcClient.GetXAuthToken(), endpoint.URL)
	if err != nil {
		return nil, diag.FromErr(fmt.Errorf("can't create dbaas v2 client: %w", err))
	}

	return client, nil
}

func validateDatastoreTypeV2(ctx context.Context, expectedDatastoreTypeEngines []string, typeID string, client *dbaas_v2.API) diag.Diagnostics {

	// no endpoint to get a datastore type in v2

	response, err := client.DatastoreType.GetDatastoreTypeList(ctx)
	if err != nil {
		return diag.FromErr(errors.New("Couldnt get datastore type list"))
	}

	if response.Errors != "" {
		log.Printf("[WARN] datastore types got with error: %s", response.Errors)

	}

	var datastoreType *dbaas_v2_common.DatastoreTypeResponse

	for _, dt := range response.DatastoreTypes {
		if dt.ID == typeID {
			datastoreType = &dt
		}
	}

	if datastoreType == nil {
		return diag.FromErr(errors.New("Couldnt get datastore type with id " + typeID))
	}
	if !containDatastoreType(expectedDatastoreTypeEngines, datastoreType.Engine) {
		return diag.FromErr(errors.New(buildDatastoreTypeErrorMessage(expectedDatastoreTypeEngines, datastoreType.Engine)))
	}

	return nil
}

func flattenDBaaSDatastoreV2ClickhouseNodeGroups(nodeGroups []dbaas_v2_ch.NodeGroupResponse) []any {

	// sort.Slice(nodeGroups, func(i, j int) bool {
	// 	return nodeGroups[i].Name < nodeGroups[j].Name
	// })

	flattenedNodeGroups := make([]any, len(nodeGroups))

	for i, ng := range nodeGroups {
		flattenedNG := map[string]any{
			"id":             ng.ID,
			"name":           ng.Name,
			"role":           ng.Role,
			"node_count":     ng.NodeCount,
			"weight":         ng.Weight,
			"has_public_ips": ng.HasPublicIPs,
			"flavor":         flattenDBaasDatastoreV2ClickhouseNodeGroupFlavor(ng.Flavor),
		}
		flattenedNodeGroups[i] = flattenedNG
	}

	return flattenedNodeGroups
}

func flattenDBaasDatastoreV2ClickhouseNodeGroupFlavor(f dbaas_v2_ch.FlavorResponse) []any {

	if f.Type == dbaas_v2_common.FlavorTypeFlexible {
		return []any{
			map[string]any{
				"type":      f.Type,
				"disk":      f.Disk,
				"ram":       f.RAM,
				"vcpus":     f.VCPUs,
				"disk_type": f.DiskType,
			},
		}
	} else {
		return []any{
			map[string]any{
				"id":   f.ID,
				"type": f.Type,
			},
		}
	}
}

func expandDBaasDatastoreV2ClickhouseNodeGroup(raw any) dbaas_v2_ch.NodeGroupCreateRequest {
	ng := raw.(map[string]any)

	req := dbaas_v2_ch.NodeGroupCreateRequest{
		Name:      ng["name"].(string),
		Role:      dbaas_v2_ch.NodeGroupRole(ng["role"].(string)),
		NodeCount: ng["node_count"].(int),
		Flavor:    expandDBaasDatastoreV2ClickhouseNodeGroupFlavor(ng["flavor"]),
	}

	if weight, ok := ng["weight"]; ok && weight != 0 {
		w := weight.(int)
		req.Weight = &w
	}

	if hasFIP, ok := ng["has_public_ips"]; ok && hasFIP != nil {
		h := hasFIP.(bool)
		req.HasPublicIPs = &h
	}
	return req
}

func expandDBaasDatastoreV2ClickhouseNodeGroupsFromSet(nodeGroups *schema.Set) []dbaas_v2_ch.NodeGroupCreateRequest {
	result := make([]dbaas_v2_ch.NodeGroupCreateRequest, 0, nodeGroups.Len())

	for _, rawGroup := range nodeGroups.List() {
		result = append(result, expandDBaasDatastoreV2ClickhouseNodeGroup(rawGroup))
	}

	return result
}

func expandDBaasDatastoreV2ClickhouseNodeGroupFlavor(raw any) dbaas_v2_ch.FlavorForNodeGroupRequest {
	flavors := raw.([]any)
	if len(flavors) == 0 {
		return dbaas_v2_ch.FlavorForNodeGroupRequest{}
	}

	data := flavors[0].(map[string]any)
	diskType := data["disk_type"].(string)
	flavorType := data["type"].(string)

	if flavorType == string(dbaas_v2_common.FlavorTypeFIXED) && diskType == "" {
		diskType = string(dbaas_v2_common.FlavorDiskLocal)
	}

	return dbaas_v2_ch.FlavorForNodeGroupRequest{
		ID:       data["id"].(string),
		Type:     dbaas_v2_common.FlavorType(flavorType),
		Disk:     data["disk"].(int),
		DiskType: dbaas_v2_common.FlavorDiskType(diskType),
		RAM:      data["ram"].(int),
		VCPUs:    data["vcpus"].(int),
	}
}

func updateClickhouseDatastoreName(ctx context.Context, d *schema.ResourceData, client *dbaas_v2.API) error {
	var updateOpts dbaas_v2_ch.DatastoreUpdateRequest
	updateOpts.Name = d.Get("name").(string)

	log.Print(msgUpdate(objectDatastore, d.Id(), updateOpts))
	_, err := client.ClickHouse.UpdateDatastore(ctx, d.Id(), updateOpts)
	if err != nil {
		return errUpdatingObject(objectDatastore, d.Id(), err)
	}

	log.Printf("[DEBUG] waiting for datastore %s to become 'ACTIVE'", d.Id())
	timeout := d.Timeout(schema.TimeoutUpdate)
	err = waiters.WaitForDBaaSDatastoreV2RunningActive(ctx, client, d.Id(), timeout)
	if err != nil {
		return errUpdatingObject(objectDatastore, d.Id(), err)
	}

	return nil
}

func updateClickhouseDatastorePassword(ctx context.Context, d *schema.ResourceData, client *dbaas_v2.API) error {
	var updateOpts dbaas_v2_ch.DatastoreUpdatePasswordRequest
	updateOpts.NewPassword = d.Get("password").(string)

	log.Print(msgUpdate(objectDatastore, d.Id(), updateOpts))
	_, err := client.ClickHouse.UpdateDatastorePassword(ctx, d.Id(), updateOpts)
	if err != nil {
		return errUpdatingObject(objectDatastore, d.Id(), err)
	}

	log.Printf("[DEBUG] waiting for datastore %s to become 'ACTIVE'", d.Id())
	timeout := d.Timeout(schema.TimeoutUpdate)
	err = waiters.WaitForDBaaSDatastoreV2RunningActive(ctx, client, d.Id(), timeout)
	if err != nil {
		return errUpdatingObject(objectDatastore, d.Id(), err)
	}

	return nil
}

func createClickhouseNodeGroup(
	ctx context.Context,
	client *dbaas_v2.API,
	datastoreID string,
	nodeGroupData any,
	timeout time.Duration,
) error {
	createOpts := expandDBaasDatastoreV2ClickhouseNodeGroup(nodeGroupData)
	extaMsg := fmt.Sprintf("create node group %+v", createOpts)

	log.Print(msgUpdate(objectDatastore, datastoreID, extaMsg))
	_, err := client.ClickHouse.CreateNodeGroup(ctx, datastoreID, createOpts)
	if err != nil {
		return errUpdatingObject(objectDatastore, datastoreID, err)
	}

	log.Printf("[DEBUG] waiting for datastore %s to become 'ACTIVE'", datastoreID)
	err = waiters.WaitForDBaaSDatastoreV2RunningActive(ctx, client, datastoreID, timeout)
	if err != nil {
		return errUpdatingObject(objectDatastore, datastoreID, err)
	}

	return nil
}

func deleteClickhouseNodeGroup(
	ctx context.Context,
	client *dbaas_v2.API,
	datastoreID string,
	nodeGroupID string,
	timeout time.Duration,
) error {

	extaMsg := fmt.Sprintf("delete node group %s", nodeGroupID)
	log.Print(msgUpdate(objectDatastore, datastoreID, extaMsg))
	err := client.ClickHouse.DeleteNodeGroup(ctx, datastoreID, nodeGroupID)
	if err != nil {
		return errUpdatingObject(objectDatastore, datastoreID, err)
	}

	log.Printf("[DEBUG] waiting for datastore %s to become 'ACTIVE'", datastoreID)
	err = waiters.WaitForDBaaSDatastoreV2RunningActive(ctx, client, datastoreID, timeout)
	if err != nil {
		return errUpdatingObject(objectDatastore, datastoreID, err)
	}

	return nil
}

func resizeClickhouseNodeGroup(
	ctx context.Context,
	client *dbaas_v2.API,
	datastoreID string,
	nodeGroupID string,
	resizeData dbaas_v2_ch.NodeGroupResizeRequest,
	timeout time.Duration,
) error {
	extaMsg := fmt.Sprintf("resize node group %s: %+v", nodeGroupID, resizeData)

	log.Print(msgUpdate(objectDatastore, datastoreID, extaMsg))

	_, err := client.ClickHouse.ResizeNodeGroup(ctx, datastoreID, nodeGroupID, resizeData)
	if err != nil {
		return errUpdatingObject(objectDatastore, datastoreID, err)
	}

	log.Printf("[DEBUG] waiting for datastore %s to become 'ACTIVE'", datastoreID)
	err = waiters.WaitForDBaaSDatastoreV2RunningActive(ctx, client, datastoreID, timeout)
	if err != nil {
		return errUpdatingObject(objectDatastore, datastoreID, err)
	}

	return nil
}
