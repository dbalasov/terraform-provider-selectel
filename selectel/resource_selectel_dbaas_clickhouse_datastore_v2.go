package selectel

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/customdiff"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	dbaas_v2 "github.com/selectel/dbaas-go/v2"
	dbaas_v2_ch "github.com/selectel/dbaas-go/v2/clickhouse"
	dbaas_v2_common "github.com/selectel/dbaas-go/v2/common"
	waiters "github.com/terraform-providers/terraform-provider-selectel/selectel/waiters/dbaas"
)

func resourceDBaaSClickhouseDatastoreV2() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceDBaaSClickhouseDatastoreV2Create,
		ReadContext:   resourceDBaaSClickhouseDatastoreV2Read,
		UpdateContext: resourceDBaaSClickHouseDatastoreV2Update,
		DeleteContext: resourceDBaaSClickHouseDatastoreV2Delete,
		CustomizeDiff: customdiff.All(
			validateV2ClickHouseDatastoreDiff,
		),
		Importer: &schema.ResourceImporter{
			StateContext: resourceDBaaSClickHouseDatastoreV2ImportState,
		},
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(60 * time.Minute),
			Update: schema.DefaultTimeout(60 * time.Minute),
			Delete: schema.DefaultTimeout(60 * time.Minute),
		},
		Schema: resourceDBaaSClickhouseDatastoreV2Schema(),
	}
}

func resourceDBaaSClickhouseDatastoreV2Create(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	dbaasClient, diagErr := getDBaaSV2Client(d, meta)
	if diagErr != nil {
		return diagErr
	}

	typeID := d.Get("type_id").(string)
	diagErr = validateDatastoreTypeV2(ctx, []string{clickhouseDatastoreType}, typeID, dbaasClient)
	if diagErr != nil {
		return diagErr
	}

	nodeGroups := expandDBaasDatastoreV2ClickhouseNodeGroupsFromSet(d.Get("node_groups").(*schema.Set))

	datastoreCreateOpts := dbaas_v2_ch.DatastoreCreateRequest{
		Name:       d.Get("name").(string),
		TypeID:     typeID,
		SubnetID:   d.Get("subnet_id").(string),
		Password:   d.Get("password").(string),
		Config:     d.Get("config").(map[string]any),
		NodeGroups: nodeGroups,
	}

	sgRaw, sgOk := d.GetOk("security_groups")
	if sgOk {
		sgSet := sgRaw.(*schema.Set)
		sg, err := resourceDBaaSDatastoreV1SecurityGroupsFromSet(sgSet)
		if err != nil {
			return diag.FromErr(errParseDatastoreV1SecurityGroups(err))
		}
		datastoreCreateOpts.SecurityGroups = sg
	}

	logGroup, logOk := d.GetOk("log_platform")
	if logOk {
		datastoreCreateOpts.LogPlatform = &dbaas_v2_ch.DatastoreLogGroup{LogGroup: logGroup.(string)}
	}

	log.Print(msgCreate(objectDatastore, datastoreCreateOpts))
	datastore, err := dbaasClient.ClickHouse.CreateDatastore(ctx, datastoreCreateOpts)
	if err != nil {
		return diag.FromErr(errCreatingObject(objectDatastore, err))
	}

	log.Printf("[DEBUG] waiting for datastore %s to become 'ACTIVE'", datastore.ID)
	timeout := d.Timeout(schema.TimeoutCreate)
	err = waiters.WaitForDBaaSDatastoreV2RunningActive(ctx, dbaasClient, datastore.ID, timeout)
	if err != nil {
		return diag.FromErr(errCreatingObject(objectDatastore, err))
	}

	d.SetId(datastore.ID)

	return resourceDBaaSClickhouseDatastoreV2Read(ctx, d, meta)
}

func resourceDBaaSClickhouseDatastoreV2Read(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	dbaasClient, diagErr := getDBaaSV2Client(d, meta)
	if diagErr != nil {
		return diagErr
	}

	log.Print(msgGet(objectDatastore, d.Id()))
	datastore, err := dbaasClient.ClickHouse.GetDatastore(ctx, d.Id())
	if err != nil {
		return diag.FromErr(errGettingObject(objectDatastore, d.Id(), err))
	}
	d.Set("name", datastore.Name)
	d.Set("status", datastore.Status)
	d.Set("state", datastore.State)
	d.Set("project_id", datastore.ProjectID)
	d.Set("subnet_id", datastore.SubnetID)
	d.Set("type_id", datastore.TypeID)

	d.Set("security_groups", datastore.SecurityGroups)
	d.Set("log_platform", datastore.LogPlatform)

	nodeGroups := flattenDBaaSDatastoreV2ClickhouseNodeGroups(datastore.NodeGroups)
	if err := d.Set("node_groups", nodeGroups); err != nil {
		log.Print(errSettingComplexAttr("node_groups", err))
	}

	// TODO: convert by getting params and use its type
	configMap := make(map[string]string)
	for key, value := range datastore.Config {
		configMap[key] = convertFieldToStringByType(value)
	}
	if err := d.Set("config", configMap); err != nil {
		log.Print(errSettingComplexAttr("config", err))
	}

	return nil
}

func resourceDBaaSClickHouseDatastoreV2Update(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	dbaasClient, diagErr := getDBaaSV2Client(d, meta)
	if diagErr != nil {
		return diagErr
	}
	timeout := d.Timeout(schema.TimeoutUpdate)

	if d.HasChange("name") {
		err := updateClickhouseDatastoreName(ctx, d, dbaasClient)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	if d.HasChange("password") {
		err := updateClickhouseDatastorePassword(ctx, d, dbaasClient)
		if err != nil {
			return diag.FromErr(err)
		}
	}

	if d.HasChange("node_groups") {
		oldRaw, newRaw := d.GetChange("node_groups")

		oldGroups := oldRaw.([]any)
		newGroups := newRaw.([]any)

		if err := reconcileClickhouseNodeGroups(
			ctx,
			dbaasClient,
			d.Id(),
			oldGroups,
			newGroups,
			timeout,
		); err != nil {
			return diag.FromErr(err)
		}

	}
	if d.HasChange("config") {
		// Update config
	}

	if d.HasChange("security_groups") {
		// Update security_groups
	}
	if d.HasChange("log_platform") {
		// Update log_platform
	}

	return resourceDBaaSClickhouseDatastoreV2Read(ctx, d, meta)
}

func reconcileClickhouseNodeGroups(
	ctx context.Context,
	client *dbaas_v2.API,
	datastoreID string,
	oldGroups []any,
	newGroups []any,
	timeout time.Duration,
) error {

	oldByName := make(map[string]map[string]any)

	for _, raw := range oldGroups {
		group := raw.(map[string]any)
		oldByName[group["name"].(string)] = group
	}

	newByName := make(map[string]map[string]any)

	for _, raw := range newGroups {
		group := raw.(map[string]any)
		newByName[group["name"].(string)] = group
	}

	// Create / update.
	for name, newGroup := range newByName {
		oldGroup, exists := oldByName[name]

		if !exists {
			if err := createClickhouseNodeGroup(
				ctx, client, datastoreID, newGroup, timeout,
			); err != nil {
				return fmt.Errorf("creating node group error: %w", err)
			}
			continue
		}

		oldID := oldGroup["id"].(string)

		if err := reconcileClickhouseNodeGroup(
			ctx, client, datastoreID, oldID, oldGroup, newGroup, timeout,
		); err != nil {
			return fmt.Errorf("reconciliation node group error: %w", err)
		}
	}

	// Delete.
	for name, oldGroup := range oldByName {
		if _, exists := newByName[name]; exists {
			continue
		}

		oldID := oldGroup["id"].(string)

		if err := deleteClickhouseNodeGroup(
			ctx, client, datastoreID, oldID, timeout,
		); err != nil {
			return fmt.Errorf("deleting node group error: %w", err)
		}
	}

	return nil
}

func reconcileClickhouseNodeGroup(
	ctx context.Context,
	client *dbaas_v2.API,
	datastoreID string,
	nodeGroupID string,
	oldGroup map[string]any,
	newGroup map[string]any,
	timeout time.Duration,
) error {
	oldNodeCount := oldGroup["node_count"].(int)
	newNodeCount := newGroup["node_count"].(int)

	// oldFlavor := expandFlavor(oldGroup["flavor"])
	// newFlavor := expandFlavor(newGroup["flavor"])

	if oldNodeCount == newNodeCount {
		return nil
	}

	req := dbaas_v2_ch.NodeGroupResizeRequest{
		NodeCount: newNodeCount,
		// Flavor:    newFlavor,
	}

	_, err := client.ClickHouse.ResizeNodeGroup(
		ctx,
		datastoreID,
		nodeGroupID,
		req,
	)

	// check fip
	// check weight

	return err
}

// func expandFlavor(raw any) Flavor {
// 	if len(raw) == 0 {
// 		return Flavor{}
// 	}

// 	data, ok := raw[0].(map[string]any)
// 	if !ok {
// 		return Flavor{}
// 	}

// 	flavor := Flavor{}

// 	if v, ok := data["id"].(string); ok {
// 		flavor.ID = v
// 	}

// 	if v, ok := data["type"].(string); ok {
// 		flavor.Type = v
// 	}

// 	if v, ok := data["disk"].(int); ok {
// 		flavor.Disk = v
// 	}

// 	if v, ok := data["ram"].(int); ok {
// 		flavor.RAM = v
// 	}

// 	if v, ok := data["vcpus"].(int); ok {
// 		flavor.VCPUs = v
// 	}

// 	return flavor
// }

func resourceDBaaSClickHouseDatastoreV2Delete(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	dbaasClient, diagErr := getDBaaSV2Client(d, meta)
	if diagErr != nil {
		return diagErr
	}

	log.Print(msgDelete(objectDatastore, d.Id()))
	err := dbaasClient.ClickHouse.DeleteDatastore(ctx, d.Id())
	if err != nil {
		return diag.FromErr(errDeletingObject(objectDatastore, d.Id(), err))
	}

	log.Printf("[DEBUG] waiting for datastore %s to become deleted", d.Id())
	timeout := d.Timeout(schema.TimeoutDelete)
	err = waiters.WaitForDBaaSDatastoreV2Deleted(ctx, dbaasClient, d.Id(), timeout)
	if err != nil {
		return diag.FromErr(errDeletingObject(objectDatastore, d.Id(), err))
	}
	return nil
}

func resourceDBaaSClickHouseDatastoreV2ImportState(_ context.Context, d *schema.ResourceData, meta any) ([]*schema.ResourceData, error) {
	config := meta.(*Config)
	if config.ProjectID == "" {
		return nil, errors.New("INFRA_PROJECT_ID must be set for the resource import")
	}
	if config.Region == "" {
		return nil, errors.New("INFRA_REGION must be set for the resource import")
	}

	d.Set("project_id", config.ProjectID)
	d.Set("region", config.Region)

	return []*schema.ResourceData{d}, nil
}

func validateV2ClickHouseDatastoreDiff(
	ctx context.Context,
	diff *schema.ResourceDiff,
	meta any,
) error {
	rawGroups, ok := diff.Get("node_groups").([]any)
	if !ok {
		return nil
	}

	for _, rawGroup := range rawGroups {
		group := rawGroup.(map[string]any)

		if err := validateV2ClickHouseNodeGroup(group); err != nil {
			return err
		}
	}

	return nil
}

func validateV2ClickHouseNodeGroup(group map[string]any) error {
	role := group["role"].(string)
	hasPublicIPs := group["has_public_ips"].(bool)
	weight := group["weight"].(int)

	switch role {
	case string(dbaas_v2_ch.NodeGroupRoleData):
		if weight <= 0 {
			return fmt.Errorf(
				"node group %q with role DATA must have weight > 0",
				group["name"],
			)
		}

	case string(dbaas_v2_ch.NodeGroupRoleKeeper):
		if hasPublicIPs {
			return fmt.Errorf(
				"node group %q with role KEEPER cannot have public IPs",
				group["name"],
			)
		}

		if weight > 0 {
			return fmt.Errorf(
				"node group %q with role KEEPER cannot have weight > 0",
				group["name"],
			)
		}
	}

	if err := validateV2ClickHouseNodeGroupFlavor(group); err != nil {
		return err
	}

	return nil
}

func validateV2ClickHouseNodeGroupFlavor(group map[string]any) error {
	rawFlavors := group["flavor"].([]any)
	if len(rawFlavors) == 0 {
		return nil
	}

	flavor := rawFlavors[0].(map[string]any)

	flavorType := flavor["type"].(string)

	switch flavorType {
	case string(dbaas_v2_common.FlavorTypeFIXED):
		if flavor["id"].(string) == "" {
			return fmt.Errorf(
				"flavor.id is required for FIXED flavor",
			)
		}

		if flavor["disk"].(int) != 0 ||
			flavor["ram"].(int) != 0 ||
			flavor["vcpus"].(int) != 0 {
			return fmt.Errorf(
				"FIXED flavor cannot specify disk, ram or vcpus",
			)
		}

		if flavor["disk_type"].(string) != "" {
			return fmt.Errorf(
				"flavor.disk_type cannot be specified for FIXED flavor",
			)
		}

	case string(dbaas_v2_common.FlavorTypeFlexible):
		if flavor["id"].(string) != "" {
			return fmt.Errorf(
				"flavor.id cannot be specified for FLEXIBLE flavor",
			)
		}

		if flavor["disk"].(int) <= 0 ||
			flavor["ram"].(int) <= 0 ||
			flavor["vcpus"].(int) <= 0 {
			return fmt.Errorf(
				"disk, ram and vcpus must be greater than 0 for FLEXIBLE flavor",
			)
		}

		if flavor["disk_type"].(string) == "" {
			return fmt.Errorf(
				"flavor.disk_type is required for FLEXIBLE flavor",
			)
		}
	}

	return nil
}
