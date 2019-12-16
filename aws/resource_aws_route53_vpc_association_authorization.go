package aws

// copied from resource_api_gateway_authorizer

import (
	"fmt"
	"log"
	"strings"
	// "time"

	"github.com/aws/aws-sdk-go/aws"
	// "github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/route53"

	// "github.com/hashicorp/terraform-plugin-sdk/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	// "github.com/hashicorp/terraform-plugin-sdk/helper/validation"
)

// const defaultAuthorizerTTL = 300

func resourceAwsRoute53CreateVPCAssociationAuthorization() *schema.Resource {
	return &schema.Resource{
		Create: resourceAwsRoute53CreateVPCAssociationAuthorizationCreate,
		Read:   resourceAwsRoute53CreateVPCAssociationAuthorizationRead,
		// Update:        resourceAwsRoute53CreateVPCAssociationAuthorizationUpdate, // is this needed?
		Delete: resourceAwsRoute53CreateVPCAssociationAuthorizationDelete,
		// CustomizeDiff: resourceAwsRoute53CreateVPCAssociationAuthorizationCustomizeDiff, // is this needed?

		Schema: map[string]*schema.Schema{
			"zone_id": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"vpc_id": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"vpc_region": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},
		},
	}
}

func resourceAwsRoute53CreateVPCAssociationAuthorizationCreate(d *schema.ResourceData, meta interface{}) error {
	// create a client session with route53
	conn := meta.(*AWSClient).r53conn

	// construct the authorization input struct
	input := route53.CreateVPCAssociationAuthorizationInput{
		HostedZoneId: aws.String(d.Get("zone_id").(string)),
		VPC: &route53.VPC{
			VPCId:     aws.String(d.Get("vpc_id").(string)),
			VPCRegion: aws.String(meta.(*AWSClient).region),
		},
	}
	// appears to allow override of client's default region by resource-specific region
	if w := d.Get("vpc_region"); w != "" {
		input.VPC.VPCRegion = aws.String(w.(string))
	}

	log.Printf("[INFO] Creating VPC Association Authorization: %s", input)
	_, err := conn.CreateVPCAssociationAuthorization(&input)
	if err != nil {
		return fmt.Errorf("Error creating VPC Association Authorization: %s", err)
	}

	// Store association id
	d.SetId(fmt.Sprintf("%s:%s", *input.HostedZoneId, *input.VPC.VPCId))

	// Not sure how to get the Refresh field sorted out as output from above
	// does not include a ChangeInfo field
	//
	// Wait until we are done initializing
	// wait := resource.StateChangeConf{
	// 	Delay:      30 * time.Second,
	// 	Pending:    []string{"PENDING"},
	// 	Target:     []string{"INSYNC"},
	// 	Timeout:    10 * time.Minute,
	// 	MinTimeout: 2 * time.Second,
	// 	Refresh: func() (result interface{}, state string, err error) {
	// 		changeRequest := &route53.GetChangeInput{
	// 			Id: aws.String(cleanChangeID(*out.ChangeInfo.Id)),
	// 		}
	// 		return resourceAwsGoRoute53Wait(conn, changeRequest)
	// 	},
	// }
	// _, err = wait.WaitForState()
	// if err != nil {
	// 	return err
	// }

	return resourceAwsRoute53CreateVPCAssociationAuthorizationRead(d, meta)
}

func resourceAwsRoute53CreateVPCAssociationAuthorizationRead(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).r53conn

	log.Printf("[INFO] Reading VPC Association Authorizations %s", d.Id())
	zoneID, vpcID, err := resourceAwsRoute53VPCAssociationAuthorizationParseId(d.Id())

	if err != nil {
		return err
	}

	vpc, err := route53GetVPCAssociation(conn, zoneID, vpcID)

	// what other errors could be returned?
	if isAWSErr(err, route53.ErrCodeNoSuchHostedZone, "") {
		log.Printf("[WARN] Route 53 Hosted Zone (%s) not found, removing from state", zoneID)
		d.SetId("")
		return nil
	}
	// ErrCodeInvalidInput case should be handled by explicit validation check within route53GetVPCAssociation
	// ErrCodeInvalidPaginationToken case is not a possibility as the NextToken optional input is not yet supported in this package

	if err != nil {
		return fmt.Errorf("error getting Route 53 VPC (%s) Association Authorization for Hosted Zone (%s): %s", vpcID, zoneID, err)
	}

	if vpc == nil {
		log.Printf("[WARN] Route 53 VPC (%s) Association Authorization for Hosted Zone (%s) not found, removing from state", vpcID, zoneID)
		d.SetId("")
		return nil
	}

	d.Set("vpc_id", vpc.VPCId)
	d.Set("vpc_region", vpc.VPCRegion)
	d.Set("zone_id", zoneID)

	return nil
}

func resourceAwsRoute53CreateVPCAssociationAuthorizationDelete(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).r53conn

	log.Printf("[INFO] Reading VPC Association Authorizations %s", d.Id())
	zoneID, vpcID, err := resourceAwsRoute53VPCAssociationAuthorizationParseId(d.Id())

	if err != nil {
		return err
	}

	log.Printf("[DEBUG] Deauthorizing Route 53 VPC (%s) Association: %s", vpcID, zoneID)

	input := &route53.DeleteVPCAssociationAuthorizationInput{
		HostedZoneId: aws.String(zoneID),
		VPC: &route53.VPC{
			VPCId:     aws.String(vpcID),
			VPCRegion: aws.String(d.Get("vpc_region").(string)),
		},
	}

	_, err = conn.DeleteVPCAssociationAuthorization(input)

	return nil
}

func resourceAwsRoute53VPCAssociationAuthorizationParseId(id string) (string, string, error) {
	parts := strings.Split(id, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("Unexpected format of ID (%q), expected ZONEID:VPCID", id)
	}
	return parts[0], parts[1], nil
}

func route53GetVPCAssociation(conn *route53.Route53, zoneID, vpcID string) (*route53.VPC, error) {
	input := &route53.ListVPCAssociationAuthorizationsInput{
		HostedZoneId: aws.String(zoneID),
		// MaxResults currently defaults to 50
		// NextToken to be implemented later
	}

	err := input.Validate()

	if err != nil {
		return nil, fmt.Errorf("Bad input %s for List VPC Association Authorizations: %s", input.GoString(), err)
	}

	output, err := conn.ListVPCAssociationAuthorizations(input)

	if err != nil {
		return nil, err
	}

	var vpc *route53.VPC
	for _, zoneVPC := range output.VPCs {
		if vpcID == aws.StringValue(zoneVPC.VPCId) {
			vpc = zoneVPC
			break
		}
	}

	return vpc, nil
}
