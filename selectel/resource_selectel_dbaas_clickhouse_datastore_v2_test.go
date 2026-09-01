package selectel

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	dbaas_v2_ch "github.com/selectel/dbaas-go/v2/clickhouse"
	dbaas_v2_common "github.com/selectel/dbaas-go/v2/common"
)

const resourceDBaaSClisckhouseDatastoreV2Name = "selectel_dbaas_clickhouse_datastore_v2.datastore_tf_acc_test_1"

func testAccCheckDBaaSV2ClickhouseDatastoreDestroy(s *terraform.State) error {

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "selectel_dbaas_clickhouse_datastore_v2" {
			continue
		}
		ctx := context.Background()
		client, err := newTestDBaaSV2Client(ctx, rs, testAccProvider)
		if err != nil {
			return err
		}

		_, err = client.ClickHouse.GetDatastore(ctx, rs.Primary.ID)
		if err == nil {
			return fmt.Errorf(
				"clickhouse datastore %s still exists after destroy",
				rs.Primary.ID,
			)
		}

	}

	return nil
}

func testAccCheckDBaaSV2ClickhouseDatastoreExists(n string, dbaasDatastore *dbaas_v2_ch.DatastoreResponse) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return errors.New("no ID is set")
		}

		ctx := context.Background()

		dbaasClient, err := newTestDBaaSV2Client(ctx, rs, testAccProvider)
		if err != nil {
			return err
		}

		datastore, err := dbaasClient.ClickHouse.GetDatastore(ctx, rs.Primary.ID)
		if err != nil {
			return err
		}

		if datastore.ID != rs.Primary.ID {
			return errors.New("datastore not found")
		}

		*dbaasDatastore = datastore

		return nil
	}
}

func TestAccDBaaSClickhouseDatastoreV2Basic(t *testing.T) {
	var dbaasDatastore dbaas_v2_ch.DatastoreResponse

	datastoreName := acctest.RandomWithPrefix("tf-acc-ds")
	updatedDatastoreName := acctest.RandomWithPrefix("tf-acc-ds-updated")

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccSelectelPreCheck(t)
			// Need to create a network by openstack operator beacause 'selectel_vpc_subnet_v2' creates a network with 'public' tag.
			// Cickhouse api denies 'You can not use subnet {subnet_id} because it is a part of the external network'.
			testAccDBaaSV2PreCheck(t)
		},
		ProviderFactories: testAccProvidersWithOpenStack,
		CheckDestroy:      testAccCheckDBaaSV2ClickhouseDatastoreDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccDBaaSClickhouseDatastoreV2Basic(datastoreName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDBaaSV2ClickhouseDatastoreExists(resourceDBaaSClisckhouseDatastoreV2Name, &dbaasDatastore),
					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "name", datastoreName),
					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "region", dbaasRegion),
					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "status", string(dbaas_v2_common.DatastoreStatusActive)),
					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "state", string(dbaas_v2_common.DatastoreStateRunning)),

					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "node_groups.#", "1"),
					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "node_groups.0.name", "shard1"),
					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "node_groups.0.node_count", "1"),
					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "node_groups.0.role", "DATA"),
					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "node_groups.0.weight", "50"),

					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "node_groups.0.flavor.0.type", "FLEXIBLE"),
					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "node_groups.0.flavor.0.vcpus", "2"),
					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "node_groups.0.flavor.0.ram", "4096"),
					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "node_groups.0.flavor.0.disk", "32"),
					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "node_groups.0.flavor.0.disk_type", "LOCAL"),

					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "security_groups.#", "1"),
					resource.TestCheckResourceAttrSet(resourceDBaaSClisckhouseDatastoreV2Name, "security_groups.0"), // first item is not empty string
				),
			},
			{
				Config: testAccDBaaSClickhouseDatastoreV2UpdateName(updatedDatastoreName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceDBaaSClisckhouseDatastoreV2Name, "name", updatedDatastoreName),
				),
			},
		},
	})
}

func testAccDBaaSClickhouseDatastoreV2Basic(datastoreName string) string {
	return fmt.Sprintf(`
provider openstack {
	tenant_id = "%s"
}

resource "openstack_networking_network_v2" "my_net" {
 	region = "%s"
  	name = "network_one"
}

resource "openstack_networking_subnet_v2" "my_subnet" {
  network_id = openstack_networking_network_v2.my_net.id
  cidr       = "192.168.1.0/24"
  ip_version = 4
  enable_dhcp = false
  name = "subnet_one"
}

data "selectel_dbaas_datastore_type_v2" "dt" {
  project_id = "%s"
  region = "%s"
  filter {
    engine = "clickhouse"
    version = "26.3.12.3"

  }
}

resource "selectel_dbaas_clickhouse_datastore_v2" "datastore_tf_acc_test_1" {
  name = "%s"
  project_id = "%s"
  region = "%s"
  type_id = "${data.selectel_dbaas_datastore_type_v2.dt.datastore_types[0].id}"
  subnet_id = "${openstack_networking_subnet_v2.my_subnet.id}"
  password = "Iu2YgYlk!ORz"
  node_groups {
    name       = "shard1" 
    role       = "DATA"
    node_count = 1
	weight     = 50

    flavor {
      type  = "FLEXIBLE"
      vcpus = 2
      ram   = 4096
      disk  = 32
      disk_type = "LOCAL"
    }
  }
}`, dbaasProjectID, dbaasRegion, dbaasProjectID, dbaasRegion, datastoreName, dbaasProjectID, dbaasRegion)
}

func testAccDBaaSClickhouseDatastoreV2UpdateName(datastoreName string) string {
	return fmt.Sprintf(`
provider openstack {
	tenant_id = "%s"
}

resource "openstack_networking_network_v2" "my_net" {
 	region = "%s"
  	name = "network_one"
}

resource "openstack_networking_subnet_v2" "my_subnet" {
  network_id = openstack_networking_network_v2.my_net.id
  cidr       = "192.168.1.0/24"
  ip_version = 4
  enable_dhcp = false
  name = "subnet_one"
}

data "selectel_dbaas_datastore_type_v2" "dt" {
  project_id = "%s"
  region = "%s"
  filter {
    engine = "clickhouse"
    version = "26.3.12.3"

  }
}

resource "selectel_dbaas_clickhouse_datastore_v2" "datastore_tf_acc_test_1" {
  name = "%s"
  project_id = "%s"
  region = "%s"
  type_id = "${data.selectel_dbaas_datastore_type_v2.dt.datastore_types[0].id}"
  subnet_id = "${openstack_networking_subnet_v2.my_subnet.id}"
  password = "Iu2YgYlk!ORz"
  node_groups {
    name       = "shard1" 
    role       = "DATA"
    node_count = 1
	weight     = 50

    flavor {
      type  = "FLEXIBLE"
      vcpus = 2
      ram   = 4096
      disk  = 32
      disk_type = "LOCAL"
    }
  }
}`, dbaasProjectID, dbaasRegion, dbaasProjectID, dbaasRegion, datastoreName, dbaasProjectID, dbaasRegion)
}
