package aws

import (
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/helper/validation"
	"github.com/terraform-providers/terraform-provider-aws/aws/internal/keyvaluetags"
)

func resourceAwsEcsCapacityProvider() *schema.Resource {
	return &schema.Resource{
		Create: resourceAwsEcsCapacityProviderCreate,
		Read:   resourceAwsEcsCapacityProviderRead,
		Update: resourceAwsEcsCapacityProviderUpdate,
		Delete: schema.Noop,
		Importer: &schema.ResourceImporter{
			State: resourceAwsEcsCapacityProviderImport,
		},
		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"arn": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"auto_scaling_group_provider": {
				Type:     schema.TypeList,
				MaxItems: 1,
				Required: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"auto_scaling_group_arn": {
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validateArn,
						},
						"managed_termination_protection": {
							Type:     schema.TypeString,
							Optional: true,
							Computed: true,
							ValidateFunc: validation.StringInSlice([]string{
								"ENABLED",
								"DISABLED",
							}, false),
						},
						"managed_scaling": {
							Type:     schema.TypeList,
							MaxItems: 1,
							Optional: true,
							Computed: true,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"maximum_scaling_step_size": {
										Type:         schema.TypeInt,
										Optional:     true,
										Computed:     true,
										ValidateFunc: validation.IntBetween(1, 10000),
									},
									"minimum_scaling_step_size": {
										Type:         schema.TypeInt,
										Optional:     true,
										Computed:     true,
										ValidateFunc: validation.IntBetween(1, 10000),
									},
									"status": {
										Type:     schema.TypeString,
										Optional: true,
										Computed: true,
										ValidateFunc: validation.StringInSlice([]string{
											"ENABLED",
											"DISABLED",
										}, false)},
									"target_capacity": {
										Type:         schema.TypeInt,
										Optional:     true,
										Computed:     true,
										ValidateFunc: validation.IntBetween(1, 100),
									},
								},
							},
						},
					},
				},
			},
			"tags": tagsSchema(),
		},
	}
}

func resourceAwsEcsCapacityProviderCreate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).ecsconn

	// `CreateCapacityProviderInput` does not accept an empty array of tags
	var tags []*ecs.Tag
	if t, ok := d.GetOk("tags"); ok {
		tags = keyvaluetags.New(t.(map[string]interface{})).IgnoreAws().EcsTags()
	}
	input := ecs.CreateCapacityProviderInput{
		Name:                     aws.String(d.Get("name").(string)),
		AutoScalingGroupProvider: expandAutoScalingGroupProvider(d.Get("auto_scaling_group_provider")),
		Tags:                     tags,
	}
	out, err := conn.CreateCapacityProvider(&input)
	// TODO figure out which errors are retryable vs not, add a resource.Retry block if necessary

	if err != nil {
		return fmt.Errorf("error creating capacity provider: %s", err)
	}

	provider := *out.CapacityProvider

	log.Printf("[DEBUG] ECS Capacity Provider created: %s", aws.StringValue(provider.CapacityProviderArn))
	d.SetId(aws.StringValue(provider.CapacityProviderArn))

	return resourceAwsEcsCapacityProviderRead(d, meta)
}

func resourceAwsEcsCapacityProviderRead(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).ecsconn

	input := &ecs.DescribeCapacityProvidersInput{
		CapacityProviders: []*string{aws.String(d.Id())},
		Include:           []*string{aws.String(ecs.ClusterFieldTags)},
	}

	output, err := conn.DescribeCapacityProviders(input)

	if err != nil {
		return fmt.Errorf("error reading ECS Capacity Provider (%s): %s", d.Id(), err)
	}

	var provider *ecs.CapacityProvider
	for _, cp := range output.CapacityProviders {
		if aws.StringValue(cp.CapacityProviderArn) == d.Id() {
			provider = cp
			break
		}
	}

	if provider == nil {
		log.Printf("[WARN] ECS Capacity Provider (%s) not found, removing from state", d.Id())
		d.SetId("")
		return nil
	}

	d.Set("arn", provider.CapacityProviderArn)
	d.Set("name", provider.Name)

	if err := d.Set("tags", keyvaluetags.EcsKeyValueTags(provider.Tags).IgnoreAws().Map()); err != nil {
		return fmt.Errorf("error setting tags: %s", err)
	}

	if err := d.Set("auto_scaling_group_provider", flattenAutoScalingGroupProvider(provider.AutoScalingGroupProvider)); err != nil {
		return fmt.Errorf("error setting autoscaling group provider: %s", err)
	}

	return nil
}

func resourceAwsEcsCapacityProviderUpdate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).ecsconn

	if d.HasChange("tags") {
		o, n := d.GetChange("tags")

		if err := keyvaluetags.EcsUpdateTags(conn, d.Id(), o, n); err != nil {
			return fmt.Errorf("error updating ECS Cluster (%s) tags: %s", d.Id(), err)
		}
	}

	return nil
}

// TODO write delete once it's implemented in the API

func resourceAwsEcsCapacityProviderImport(d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
	d.Set("name", d.Id())
	d.SetId(arn.ARN{
		Partition: meta.(*AWSClient).partition,
		Region:    meta.(*AWSClient).region,
		AccountID: meta.(*AWSClient).accountid,
		Service:   "ecs",
		Resource:  fmt.Sprintf("capacity-provider/%s", d.Id()),
	}.String())
	return []*schema.ResourceData{d}, nil
}

func expandAutoScalingGroupProvider(configured interface{}) *ecs.AutoScalingGroupProvider {
	prov := ecs.AutoScalingGroupProvider{}

	p := configured.([]interface{})[0].(map[string]interface{})
	arn := p["auto_scaling_group_arn"].(string)
	prov.AutoScalingGroupArn = aws.String(arn)

	if mtp := p["managed_termination_protection"].(string); len(mtp) > 0 {
		prov.ManagedTerminationProtection = aws.String(mtp)
	}

	if val := p["managed_scaling"].([]interface{}); len(val) > 0 {
		if ms, ok := val[0].(map[string]interface{}); ok {
			managedScaling := ecs.ManagedScaling{}

			if val, ok := ms["maximum_scaling_step_size"].(int); ok && val != 0 {
				managedScaling.MaximumScalingStepSize = aws.Int64(int64(val))
			}
			if val, ok := ms["minimum_scaling_step_size"].(int); ok && val != 0 {
				managedScaling.MinimumScalingStepSize = aws.Int64(int64(val))
			}
			if val, ok := ms["status"].(string); ok && len(val) > 0 {
				managedScaling.Status = aws.String(val)
			}
			if val, ok := ms["target_capacity"].(int); ok && val != 0 {
				managedScaling.TargetCapacity = aws.Int64(int64(val))
			}
			prov.ManagedScaling = &managedScaling
		}
	}

	return &prov
}

func flattenAutoScalingGroupProvider(provider *ecs.AutoScalingGroupProvider) []map[string]interface{} {
	if provider == nil {
		return nil
	}

	result := make([]map[string]interface{}, 0)
	p := make(map[string]interface{}, 0)
	p["auto_scaling_group_arn"] = aws.StringValue(provider.AutoScalingGroupArn)
	p["managed_termination_protection"] = aws.StringValue(provider.ManagedTerminationProtection)

	ms := make(map[string]interface{}, 0)
	msl := make([]map[string]interface{}, 0)
	ms["maximum_scaling_step_size"] = aws.Int64Value(provider.ManagedScaling.MaximumScalingStepSize)
	ms["minimum_scaling_step_size"] = aws.Int64Value(provider.ManagedScaling.MinimumScalingStepSize)
	ms["status"] = aws.StringValue(provider.ManagedScaling.Status)
	ms["target_capacity"] = aws.Int64Value(provider.ManagedScaling.TargetCapacity)
	msl = append(msl, ms)

	p["managed_scaling"] = msl

	result = append(result, p)
	return result
}
