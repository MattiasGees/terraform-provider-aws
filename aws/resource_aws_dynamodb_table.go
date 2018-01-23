package aws

import (
	"bytes"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/hashicorp/terraform/helper/hashcode"
	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
)

func resourceAwsDynamoDbTable() *schema.Resource {
	return &schema.Resource{
		Create: resourceAwsDynamoDbTableCreate,
		Read:   resourceAwsDynamoDbTableRead,
		Update: resourceAwsDynamoDbTableUpdate,
		Delete: resourceAwsDynamoDbTableDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		SchemaVersion: 1,
		MigrateState:  resourceAwsDynamoDbTableMigrateState,

		Schema: map[string]*schema.Schema{
			"arn": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"hash_key": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"range_key": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},
			"write_capacity": {
				Type:     schema.TypeInt,
				Required: true,
			},
			"read_capacity": {
				Type:     schema.TypeInt,
				Required: true,
			},
			"attribute": {
				Type:     schema.TypeSet,
				Required: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Required: true,
						},
						"type": {
							Type:     schema.TypeString,
							Required: true,
						},
					},
				},
				Set: func(v interface{}) int {
					var buf bytes.Buffer
					m := v.(map[string]interface{})
					buf.WriteString(fmt.Sprintf("%s-", m["name"].(string)))
					return hashcode.String(buf.String())
				},
			},
			"ttl": {
				Type:     schema.TypeSet,
				Optional: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"attribute_name": {
							Type:     schema.TypeString,
							Required: true,
						},
						"enabled": {
							Type:     schema.TypeBool,
							Required: true,
						},
					},
				},
			},
			"local_secondary_index": {
				Type:     schema.TypeSet,
				Optional: true,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Required: true,
						},
						"range_key": {
							Type:     schema.TypeString,
							Required: true,
						},
						"projection_type": {
							Type:     schema.TypeString,
							Required: true,
						},
						"non_key_attributes": {
							Type:     schema.TypeList,
							Optional: true,
							Elem:     &schema.Schema{Type: schema.TypeString},
						},
					},
				},
				Set: func(v interface{}) int {
					var buf bytes.Buffer
					m := v.(map[string]interface{})
					buf.WriteString(fmt.Sprintf("%s-", m["name"].(string)))
					return hashcode.String(buf.String())
				},
			},
			"global_secondary_index": {
				Type:     schema.TypeSet,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Required: true,
						},
						"write_capacity": {
							Type:     schema.TypeInt,
							Required: true,
						},
						"read_capacity": {
							Type:     schema.TypeInt,
							Required: true,
						},
						"hash_key": {
							Type:     schema.TypeString,
							Required: true,
						},
						"range_key": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"projection_type": {
							Type:     schema.TypeString,
							Required: true,
						},
						"non_key_attributes": {
							Type:     schema.TypeList,
							Optional: true,
							Elem:     &schema.Schema{Type: schema.TypeString},
						},
					},
				},
			},
			"stream_enabled": {
				Type:     schema.TypeBool,
				Optional: true,
				Computed: true,
			},
			"stream_view_type": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
				StateFunc: func(v interface{}) string {
					value := v.(string)
					return strings.ToUpper(value)
				},
				ValidateFunc: validateStreamViewType,
			},
			"stream_arn": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"stream_label": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"tags": tagsSchema(),
		},
	}
}

func resourceAwsDynamoDbTableCreate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).dynamodbconn

	keySchemaMap := map[string]interface{}{
		"hash_key": d.Get("hash_key").(string),
	}
	if v, ok := d.GetOk("range_key"); ok {
		keySchemaMap["range_key"] = v.(string)
	}

	log.Printf("[DEBUG] Creating DynamoDB table with key schema: %#v", keySchemaMap)

	req := &dynamodb.CreateTableInput{
		TableName: aws.String(d.Get("name").(string)),
		ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
			ReadCapacityUnits:  aws.Int64(int64(d.Get("read_capacity").(int))),
			WriteCapacityUnits: aws.Int64(int64(d.Get("write_capacity").(int))),
		},
		KeySchema: expandDynamoDbKeySchema(keySchemaMap),
	}

	if v, ok := d.GetOk("attribute"); ok {
		aSet := v.(*schema.Set)
		req.AttributeDefinitions = expandDynamoDbAttributes(aSet.List())
	}

	if v, ok := d.GetOk("local_secondary_index"); ok {
		lsiSet := v.(*schema.Set)
		req.LocalSecondaryIndexes = expandDynamoDbLocalSecondaryIndexes(lsiSet.List(), keySchemaMap)
	}

	if v, ok := d.GetOk("global_secondary_index"); ok {
		globalSecondaryIndexes := []*dynamodb.GlobalSecondaryIndex{}
		gsiSet := v.(*schema.Set)
		for _, gsiObject := range gsiSet.List() {
			gsi := gsiObject.(map[string]interface{})
			gsiObject := expandDynamoDbGlobalSecondaryIndex(gsi)
			globalSecondaryIndexes = append(globalSecondaryIndexes, gsiObject)
		}
		req.GlobalSecondaryIndexes = globalSecondaryIndexes
	}

	if v, ok := d.GetOk("stream_enabled"); ok {
		req.StreamSpecification = &dynamodb.StreamSpecification{
			StreamEnabled:  aws.Bool(v.(bool)),
			StreamViewType: aws.String(d.Get("stream_view_type").(string)),
		}
	}

	var output *dynamodb.CreateTableOutput
	err := resource.Retry(1*time.Minute, func() *resource.RetryError {
		var err error
		output, err = conn.CreateTable(req)
		if err != nil {
			if isAWSErr(err, "ThrottlingException", "") {
				return resource.RetryableError(err)
			}
			if isAWSErr(err, dynamodb.ErrCodeLimitExceededException, "can be created, updated, or deleted simultaneously") {
				return resource.RetryableError(err)
			}
			if isAWSErr(err, dynamodb.ErrCodeLimitExceededException, "indexed tables that can be created simultaneously") {
				return resource.RetryableError(err)
			}

			return resource.NonRetryableError(err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	d.SetId(*output.TableDescription.TableName)
	d.Set("arn", output.TableDescription.TableArn)

	if err := waitForDynamoDbTableToBeActive(d.Id(), conn); err != nil {
		return err
	}

	return resourceAwsDynamoDbTableUpdate(d, meta)
}

func resourceAwsDynamoDbTableUpdate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).dynamodbconn

	// Cannot create or delete index while updating table IOPS
	// so we update IOPS separately
	if (d.HasChange("read_capacity") || d.HasChange("write_capacity")) && !d.IsNewResource() {
		_, err := conn.UpdateTable(&dynamodb.UpdateTableInput{
			TableName: aws.String(d.Id()),
			ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
				ReadCapacityUnits:  aws.Int64(int64(d.Get("read_capacity").(int))),
				WriteCapacityUnits: aws.Int64(int64(d.Get("write_capacity").(int))),
			},
		})
		if err != nil {
			return err
		}
		if err := waitForDynamoDbTableToBeActive(d.Id(), conn); err != nil {
			return fmt.Errorf("Error waiting for Dynamo DB Table update: %s", err)
		}
	}

	if (d.HasChange("stream_enabled") || d.HasChange("stream_view_type")) && !d.IsNewResource() {
		input := &dynamodb.UpdateTableInput{
			TableName: aws.String(d.Id()),
			StreamSpecification: &dynamodb.StreamSpecification{
				StreamEnabled:  aws.Bool(d.Get("stream_enabled").(bool)),
				StreamViewType: aws.String(d.Get("stream_view_type").(string)),
			},
		}
		_, err := conn.UpdateTable(input)
		if err != nil {
			return err
		}

		if err := waitForDynamoDbTableToBeActive(d.Id(), conn); err != nil {
			return fmt.Errorf("Error waiting for Dynamo DB Table update: %s", err)
		}
	}

	if d.HasChange("global_secondary_index") && !d.IsNewResource() {
		var attributes []*dynamodb.AttributeDefinition
		if v, ok := d.GetOk("attribute"); ok {
			attributes = expandDynamoDbAttributes(v.(*schema.Set).List())
		}

		o, n := d.GetChange("global_secondary_index")
		ops := diffDynamoDbGSI(o.(*schema.Set).List(), n.(*schema.Set).List())

		input := &dynamodb.UpdateTableInput{
			TableName:            aws.String(d.Id()),
			AttributeDefinitions: attributes,
		}

		// Only 1 online index can be created or deleted simultaneously per table
		for _, op := range ops {
			input.GlobalSecondaryIndexUpdates = []*dynamodb.GlobalSecondaryIndexUpdate{op}
			_, err := conn.UpdateTable(input)
			if err != nil {
				return err
			}
			if op.Create != nil {
				idxName := *op.Create.IndexName
				if err := waitForDynamoDbGSIToBeActive(d.Id(), idxName, conn); err != nil {
					return fmt.Errorf("Error waiting for Dynamo DB GSI %q to be created: %s", idxName, err)
				}
			}
			if op.Update != nil {
				idxName := *op.Update.IndexName
				if err := waitForDynamoDbGSIToBeActive(d.Id(), idxName, conn); err != nil {
					return fmt.Errorf("Error waiting for Dynamo DB GSI %q to be updated: %s", idxName, err)
				}
			}
			if op.Delete != nil {
				idxName := *op.Delete.IndexName
				if err := waitForDynamoDbGSIToBeDeleted(d.Id(), idxName, conn); err != nil {
					return fmt.Errorf("Error waiting for Dynamo DB GSI %q to be deleted: %s", idxName, err)
				}
			}
		}

		if err := waitForDynamoDbTableToBeActive(d.Id(), conn); err != nil {
			return fmt.Errorf("Error waiting for Dynamo DB Table op: %s", err)
		}

	}

	if d.HasChange("ttl") {
		if err := updateDynamoDbTimeToLive(d, conn); err != nil {
			log.Printf("[DEBUG] Error updating table TimeToLive: %s", err)
			return err
		}
	}

	if d.HasChange("tags") {
		if err := setTagsDynamoDb(conn, d); err != nil {
			return err
		}
	}

	return resourceAwsDynamoDbTableRead(d, meta)
}

func resourceAwsDynamoDbTableRead(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).dynamodbconn

	result, err := conn.DescribeTable(&dynamodb.DescribeTableInput{
		TableName: aws.String(d.Id()),
	})

	if err != nil {
		if isAWSErr(err, "ResourceNotFoundException", "") {
			log.Printf("[WARN] Dynamodb Table (%s) not found, error code (404)", d.Id())
			d.SetId("")
			return nil
		}
		return err
	}

	err = flattenAwsDynamoDbTableResource(d, result.Table)
	if err != nil {
		return err
	}

	ttlOut, err := conn.DescribeTimeToLive(&dynamodb.DescribeTimeToLiveInput{
		TableName: aws.String(d.Id()),
	})
	if err != nil {
		return err
	}
	if ttlOut.TimeToLiveDescription != nil {
		err := d.Set("ttl", flattenDynamoDbTtl(ttlOut.TimeToLiveDescription))
		if err != nil {
			return err
		}
	}

	tags, err := readDynamoDbTableTags(d.Get("arn").(string), conn)
	if err != nil {
		return err
	}
	d.Set("tags", tags)

	return nil
}

func resourceAwsDynamoDbTableDelete(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).dynamodbconn

	if err := waitForDynamoDbTableToBeActive(d.Id(), conn); err != nil {
		return fmt.Errorf("Error waiting for Dynamo DB Table update: %s", err)
	}

	log.Printf("[DEBUG] DynamoDB delete table: %s", d.Id())

	_, err := conn.DeleteTable(&dynamodb.DeleteTableInput{
		TableName: aws.String(d.Id()),
	})
	if err != nil {
		return err
	}

	stateConf := resource.StateChangeConf{
		Pending: []string{
			dynamodb.TableStatusActive,
			dynamodb.TableStatusDeleting,
		},
		Target:  []string{},
		Timeout: 5 * time.Minute,
		Refresh: func() (interface{}, string, error) {
			out, err := conn.DescribeTable(&dynamodb.DescribeTableInput{
				TableName: aws.String(d.Id()),
			})
			if err != nil {
				if isAWSErr(err, "ResourceNotFoundException", "") {
					return nil, "", nil
				}

				return 42, "", err
			}
			table := out.Table

			return table, *table.TableStatus, nil
		},
	}
	_, err = stateConf.WaitForState()
	return err
}

func updateDynamoDbTimeToLive(d *schema.ResourceData, conn *dynamodb.DynamoDB) error {
	toBeEnabled := false
	attributeName := ""

	o, n := d.GetChange("ttl")
	newTtl, ok := n.(*schema.Set)
	blockExists := ok && newTtl.Len() > 0

	if blockExists {
		ttlList := newTtl.List()
		ttlMap := ttlList[0].(map[string]interface{})
		attributeName = ttlMap["attribute_name"].(string)
		toBeEnabled = ttlMap["enabled"].(bool)

	} else if !d.IsNewResource() {
		oldTtlList := o.(*schema.Set).List()
		ttlMap := oldTtlList[0].(map[string]interface{})
		attributeName = ttlMap["attribute_name"].(string)
		toBeEnabled = false
	}

	if attributeName != "" {
		_, err := conn.UpdateTimeToLive(&dynamodb.UpdateTimeToLiveInput{
			TableName: aws.String(d.Id()),
			TimeToLiveSpecification: &dynamodb.TimeToLiveSpecification{
				AttributeName: aws.String(attributeName),
				Enabled:       aws.Bool(toBeEnabled),
			},
		})
		if err != nil {
			if isAWSErr(err, "ValidationException", "TimeToLive is already disabled") {
				return nil
			}
			return err
		}

		err = waitForDynamoDbTtlUpdateToBeCompleted(d.Id(), toBeEnabled, conn)
		if err != nil {
			return fmt.Errorf("Error waiting for Dynamo DB TimeToLive to be updated: %s", err)
		}
	}

	return nil
}

func readDynamoDbTableTags(arn string, conn *dynamodb.DynamoDB) (map[string]string, error) {
	output, err := conn.ListTagsOfResource(&dynamodb.ListTagsOfResourceInput{
		ResourceArn: aws.String(arn),
	})
	if err != nil {
		return nil, fmt.Errorf("Error reading tags from dynamodb resource: %s", err)
	}

	result := tagsToMapDynamoDb(output.Tags)

	// TODO Read NextToken if available

	return result, nil
}

// Waiters

func waitForDynamoDbGSIToBeActive(tableName string, gsiName string, conn *dynamodb.DynamoDB) error {
	stateConf := resource.StateChangeConf{
		Pending: []string{
			dynamodb.IndexStatusCreating,
			dynamodb.IndexStatusUpdating,
		},
		Target:  []string{dynamodb.IndexStatusActive},
		Timeout: 10 * time.Minute,
		Refresh: func() (interface{}, string, error) {
			result, err := conn.DescribeTable(&dynamodb.DescribeTableInput{
				TableName: aws.String(tableName),
			})
			if err != nil {
				return 42, "", err
			}

			table := result.Table

			// Find index
			var targetGSI *dynamodb.GlobalSecondaryIndexDescription
			for _, gsi := range table.GlobalSecondaryIndexes {
				if *gsi.IndexName == gsiName {
					targetGSI = gsi
				}
			}

			if targetGSI != nil {
				return table, *targetGSI.IndexStatus, nil
			}

			return nil, "", nil
		},
	}
	_, err := stateConf.WaitForState()
	return err
}

func waitForDynamoDbGSIToBeDeleted(tableName string, gsiName string, conn *dynamodb.DynamoDB) error {
	stateConf := resource.StateChangeConf{
		Pending: []string{
			dynamodb.IndexStatusActive,
			dynamodb.IndexStatusDeleting,
		},
		Target:  []string{},
		Timeout: 10 * time.Minute,
		Refresh: func() (interface{}, string, error) {
			result, err := conn.DescribeTable(&dynamodb.DescribeTableInput{
				TableName: aws.String(tableName),
			})
			if err != nil {
				return 42, "", err
			}

			table := result.Table

			// Find index
			var targetGSI *dynamodb.GlobalSecondaryIndexDescription
			for _, gsi := range table.GlobalSecondaryIndexes {
				if *gsi.IndexName == gsiName {
					targetGSI = gsi
				}
			}

			if targetGSI == nil {
				return nil, "", nil
			}

			return targetGSI, *targetGSI.IndexStatus, nil
		},
	}
	_, err := stateConf.WaitForState()
	return err
}

func waitForDynamoDbTableToBeActive(tableName string, conn *dynamodb.DynamoDB) error {
	stateConf := resource.StateChangeConf{
		Pending: []string{dynamodb.TableStatusCreating, dynamodb.TableStatusUpdating},
		Target:  []string{dynamodb.TableStatusActive},
		Timeout: 5 * time.Minute,
		Refresh: func() (interface{}, string, error) {
			result, err := conn.DescribeTable(&dynamodb.DescribeTableInput{
				TableName: aws.String(tableName),
			})
			if err != nil {
				return 42, "", err
			}

			return result, *result.Table.TableStatus, nil
		},
	}
	_, err := stateConf.WaitForState()

	return err
}

func waitForDynamoDbTtlUpdateToBeCompleted(tableName string, toEnable bool, conn *dynamodb.DynamoDB) error {
	pending := []string{
		dynamodb.TimeToLiveStatusEnabled,
		dynamodb.TimeToLiveStatusDisabling,
	}
	target := []string{dynamodb.TimeToLiveStatusDisabled}

	if toEnable {
		pending = []string{
			dynamodb.TimeToLiveStatusDisabled,
			dynamodb.TimeToLiveStatusEnabling,
		}
		target = []string{dynamodb.TimeToLiveStatusEnabled}
	}

	stateConf := resource.StateChangeConf{
		Pending: pending,
		Target:  target,
		Timeout: 10 * time.Second,
		Refresh: func() (interface{}, string, error) {
			result, err := conn.DescribeTimeToLive(&dynamodb.DescribeTimeToLiveInput{
				TableName: aws.String(tableName),
			})
			if err != nil {
				return 42, "", err
			}

			ttlDesc := result.TimeToLiveDescription

			return result, *ttlDesc.TimeToLiveStatus, nil
		},
	}

	_, err := stateConf.WaitForState()
	return err

}

func diffDynamoDbGSI(oldGsi, newGsi []interface{}) (ops []*dynamodb.GlobalSecondaryIndexUpdate) {
	// Transform slices into maps
	oldGsis := make(map[string]interface{})
	for _, gsidata := range oldGsi {
		m := gsidata.(map[string]interface{})
		oldGsis[m["name"].(string)] = m
	}
	newGsis := make(map[string]interface{})
	for _, gsidata := range newGsi {
		m := gsidata.(map[string]interface{})
		newGsis[m["name"].(string)] = m
	}

	for _, data := range newGsi {
		newMap := data.(map[string]interface{})
		newName := newMap["name"].(string)

		if _, exists := oldGsis[newName]; !exists {
			m := data.(map[string]interface{})
			idxName := m["name"].(string)

			ops = append(ops, &dynamodb.GlobalSecondaryIndexUpdate{
				Create: &dynamodb.CreateGlobalSecondaryIndexAction{
					IndexName:             aws.String(idxName),
					KeySchema:             expandDynamoDbKeySchema(m),
					ProvisionedThroughput: expandDynamoDbProvisionedThroughput(m),
					Projection:            expandDynamoDbProjection(m),
				},
			})
		}
	}

	for _, data := range oldGsi {
		oldMap := data.(map[string]interface{})
		oldName := oldMap["name"].(string)

		newData, exists := newGsis[oldName]
		if exists {
			newMap := newData.(map[string]interface{})
			idxName := newMap["name"].(string)

			oldWriteCapacity, oldReadCapacity := oldMap["write_capacity"].(int), oldMap["read_capacity"].(int)
			newWriteCapacity, newReadCapacity := newMap["write_capacity"].(int), newMap["read_capacity"].(int)
			capacityHasChanged := (oldWriteCapacity != newWriteCapacity || oldReadCapacity != newReadCapacity)

			oldAttributes := map[string]interface{}{
				"hash_key":           oldMap["hash_key"].(string),
				"range_key":          oldMap["range_key"].(string),
				"projection_type":    oldMap["projection_type"].(string),
				"non_key_attributes": oldMap["non_key_attributes"].([]interface{}),
			}
			newAttributes := map[string]interface{}{
				"hash_key":           newMap["hash_key"].(string),
				"range_key":          newMap["range_key"].(string),
				"projection_type":    newMap["projection_type"].(string),
				"non_key_attributes": newMap["non_key_attributes"].([]interface{}),
			}
			otherAttributesChanged := !reflect.DeepEqual(oldAttributes, newAttributes)

			if capacityHasChanged && !otherAttributesChanged {
				update := &dynamodb.GlobalSecondaryIndexUpdate{
					Update: &dynamodb.UpdateGlobalSecondaryIndexAction{
						IndexName:             aws.String(idxName),
						ProvisionedThroughput: expandDynamoDbProvisionedThroughput(newMap),
					},
				}
				ops = append(ops, update)
			} else {
				// Other attributes cannot be updated
				ops = append(ops, &dynamodb.GlobalSecondaryIndexUpdate{
					Delete: &dynamodb.DeleteGlobalSecondaryIndexAction{
						IndexName: aws.String(idxName),
					},
				})

				ops = append(ops, &dynamodb.GlobalSecondaryIndexUpdate{
					Create: &dynamodb.CreateGlobalSecondaryIndexAction{
						IndexName:             aws.String(idxName),
						KeySchema:             expandDynamoDbKeySchema(newMap),
						ProvisionedThroughput: expandDynamoDbProvisionedThroughput(newMap),
						Projection:            expandDynamoDbProjection(newMap),
					},
				})
			}
		} else {
			idxName := oldName
			ops = append(ops, &dynamodb.GlobalSecondaryIndexUpdate{
				Delete: &dynamodb.DeleteGlobalSecondaryIndexAction{
					IndexName: aws.String(idxName),
				},
			})
		}
	}
	return
}

// Expanders + flatteners

func flattenDynamoDbTtl(ttlDesc *dynamodb.TimeToLiveDescription) []interface{} {
	m := map[string]interface{}{}
	if ttlDesc.AttributeName != nil {
		m["attribute_name"] = *ttlDesc.AttributeName
		if ttlDesc.TimeToLiveStatus != nil {
			m["enabled"] = (*ttlDesc.TimeToLiveStatus == dynamodb.TimeToLiveStatusEnabled)
		}
	}
	if len(m) > 0 {
		return []interface{}{m}
	}

	return []interface{}{}
}

func flattenAwsDynamoDbTableResource(d *schema.ResourceData, table *dynamodb.TableDescription) error {
	d.Set("write_capacity", table.ProvisionedThroughput.WriteCapacityUnits)
	d.Set("read_capacity", table.ProvisionedThroughput.ReadCapacityUnits)

	attributes := []interface{}{}
	for _, attrdef := range table.AttributeDefinitions {
		attribute := map[string]string{
			"name": *attrdef.AttributeName,
			"type": *attrdef.AttributeType,
		}
		attributes = append(attributes, attribute)
		log.Printf("[DEBUG] Added Attribute: %s", attribute["name"])
	}

	d.Set("attribute", attributes)
	d.Set("name", table.TableName)

	for _, attribute := range table.KeySchema {
		if *attribute.KeyType == "HASH" {
			d.Set("hash_key", attribute.AttributeName)
		}

		if *attribute.KeyType == "RANGE" {
			d.Set("range_key", attribute.AttributeName)
		}
	}

	lsiList := make([]map[string]interface{}, 0, len(table.LocalSecondaryIndexes))
	for _, lsiObject := range table.LocalSecondaryIndexes {
		lsi := map[string]interface{}{
			"name":            *lsiObject.IndexName,
			"projection_type": *lsiObject.Projection.ProjectionType,
		}

		for _, attribute := range lsiObject.KeySchema {

			if *attribute.KeyType == "RANGE" {
				lsi["range_key"] = *attribute.AttributeName
			}
		}
		nkaList := make([]string, len(lsiObject.Projection.NonKeyAttributes))
		for _, nka := range lsiObject.Projection.NonKeyAttributes {
			nkaList = append(nkaList, *nka)
		}
		lsi["non_key_attributes"] = nkaList

		lsiList = append(lsiList, lsi)
	}

	err := d.Set("local_secondary_index", lsiList)
	if err != nil {
		return err
	}

	gsiList := make([]map[string]interface{}, 0, len(table.GlobalSecondaryIndexes))
	for _, gsiObject := range table.GlobalSecondaryIndexes {
		gsi := map[string]interface{}{
			"write_capacity": *gsiObject.ProvisionedThroughput.WriteCapacityUnits,
			"read_capacity":  *gsiObject.ProvisionedThroughput.ReadCapacityUnits,
			"name":           *gsiObject.IndexName,
		}

		for _, attribute := range gsiObject.KeySchema {
			if *attribute.KeyType == "HASH" {
				gsi["hash_key"] = *attribute.AttributeName
			}

			if *attribute.KeyType == "RANGE" {
				gsi["range_key"] = *attribute.AttributeName
			}
		}

		gsi["projection_type"] = *(gsiObject.Projection.ProjectionType)

		nonKeyAttrs := make([]string, 0, len(gsiObject.Projection.NonKeyAttributes))
		for _, nonKeyAttr := range gsiObject.Projection.NonKeyAttributes {
			nonKeyAttrs = append(nonKeyAttrs, *nonKeyAttr)
		}
		gsi["non_key_attributes"] = nonKeyAttrs

		gsiList = append(gsiList, gsi)
	}

	if table.StreamSpecification != nil {
		d.Set("stream_view_type", table.StreamSpecification.StreamViewType)
		d.Set("stream_enabled", table.StreamSpecification.StreamEnabled)
		d.Set("stream_arn", table.LatestStreamArn)
		d.Set("stream_label", table.LatestStreamLabel)
	}

	err = d.Set("global_secondary_index", gsiList)
	if err != nil {
		return err
	}

	d.Set("arn", table.TableArn)

	return nil
}

func expandDynamoDbAttributes(cfg []interface{}) []*dynamodb.AttributeDefinition {
	attributes := make([]*dynamodb.AttributeDefinition, len(cfg), len(cfg))
	for i, attribute := range cfg {
		attr := attribute.(map[string]interface{})
		attributes[i] = &dynamodb.AttributeDefinition{
			AttributeName: aws.String(attr["name"].(string)),
			AttributeType: aws.String(attr["type"].(string)),
		}
	}
	return attributes
}

// TODO: Explain keySchemaM; this is odd and shouldn't be here!
func expandDynamoDbLocalSecondaryIndexes(cfg []interface{}, keySchemaM map[string]interface{}) []*dynamodb.LocalSecondaryIndex {
	indexes := make([]*dynamodb.LocalSecondaryIndex, len(cfg), len(cfg))
	for i, lsi := range cfg {
		m := lsi.(map[string]interface{})
		idxName := m["name"].(string)

		m["hash_key"] = keySchemaM["hash_key"]

		indexes[i] = &dynamodb.LocalSecondaryIndex{
			IndexName:  aws.String(idxName),
			KeySchema:  expandDynamoDbKeySchema(m),
			Projection: expandDynamoDbProjection(m),
		}
	}
	return indexes
}

func expandDynamoDbGlobalSecondaryIndex(data map[string]interface{}) *dynamodb.GlobalSecondaryIndex {
	return &dynamodb.GlobalSecondaryIndex{
		IndexName:             aws.String(data["name"].(string)),
		KeySchema:             expandDynamoDbKeySchema(data),
		Projection:            expandDynamoDbProjection(data),
		ProvisionedThroughput: expandDynamoDbProvisionedThroughput(data),
	}
}

func expandDynamoDbProvisionedThroughput(data map[string]interface{}) *dynamodb.ProvisionedThroughput {
	return &dynamodb.ProvisionedThroughput{
		WriteCapacityUnits: aws.Int64(int64(data["write_capacity"].(int))),
		ReadCapacityUnits:  aws.Int64(int64(data["read_capacity"].(int))),
	}
}

func expandDynamoDbProjection(data map[string]interface{}) *dynamodb.Projection {
	projection := &dynamodb.Projection{
		ProjectionType: aws.String(data["projection_type"].(string)),
	}

	if data["projection_type"] == "INCLUDE" {
		nonKeyAttributes := []*string{}
		for _, attr := range data["non_key_attributes"].([]interface{}) {
			nonKeyAttributes = append(nonKeyAttributes, aws.String(attr.(string)))
		}
		projection.NonKeyAttributes = nonKeyAttributes
	}

	return projection
}

func expandDynamoDbKeySchema(data map[string]interface{}) []*dynamodb.KeySchemaElement {
	keySchema := []*dynamodb.KeySchemaElement{}

	log.Printf("[DEBUG] Expanding DynamoDB Key Schema: %#v", data)

	if v, ok := data["hash_key"]; ok && v != nil && v != "" {
		keySchema = append(keySchema, &dynamodb.KeySchemaElement{
			AttributeName: aws.String(v.(string)),
			KeyType:       aws.String("HASH"),
		})
	}

	if v, ok := data["range_key"]; ok && v != nil && v != "" {
		keySchema = append(keySchema, &dynamodb.KeySchemaElement{
			AttributeName: aws.String(v.(string)),
			KeyType:       aws.String("RANGE"),
		})
	}

	return keySchema
}
