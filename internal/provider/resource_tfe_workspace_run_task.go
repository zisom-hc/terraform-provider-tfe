// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tfe "github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

func workspaceRunTaskEnforcementLevels() []string {
	return []string{
		string(tfe.Advisory),
		string(tfe.Mandatory),
	}
}

func workspaceRunTaskStages() []string {
	return []string{
		string(tfe.PrePlan),
		string(tfe.PostPlan),
		string(tfe.PreApply),
		string(tfe.PostApply),
	}
}

// nolint: unparam
// Helper function to turn a slice of strings into an english sentence for documentation
func sentenceList(items []string, prefix string, suffix string, conjunction string) string {
	var b strings.Builder
	for i, v := range items {
		fmt.Fprint(&b, prefix, v, suffix)
		if i < len(items)-1 {
			if i < len(items)-2 {
				fmt.Fprint(&b, ", ")
			} else {
				fmt.Fprintf(&b, " %s ", conjunction)
			}
		}
	}
	return b.String()
}

type resourceWorkspaceRunTask struct {
	config ConfiguredClient
}

var _ resource.Resource = &resourceWorkspaceRunTask{}
var _ resource.ResourceWithConfigure = &resourceWorkspaceRunTask{}
var _ resource.ResourceWithImportState = &resourceWorkspaceRunTask{}

func NewWorkspaceRunTaskResource() resource.Resource {
	return &resourceWorkspaceRunTask{}
}

type modelTFEWorkspaceRunTaskV0 struct {
	ID               types.String `tfsdk:"id"`
	WorkspaceID      types.String `tfsdk:"workspace_id"`
	TaskID           types.String `tfsdk:"task_id"`
	EnforcementLevel types.String `tfsdk:"enforcement_level"`
	Stage            types.String `tfsdk:"stage"`
}

func modelFromTFEWorkspaceRunTask(v *tfe.WorkspaceRunTask) modelTFEWorkspaceRunTaskV0 {
	return modelTFEWorkspaceRunTaskV0{
		ID:               types.StringValue(v.ID),
		WorkspaceID:      types.StringValue(v.Workspace.ID),
		TaskID:           types.StringValue(v.RunTask.ID),
		EnforcementLevel: types.StringValue(string(v.EnforcementLevel)),
		Stage:            types.StringValue(string(v.Stage)),
	}
}

func (r *resourceWorkspaceRunTask) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_run_task"
}

// Configure implements resource.ResourceWithConfigure
func (r *resourceWorkspaceRunTask) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(ConfiguredClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected resource Configure type",
			fmt.Sprintf("Expected tfe.ConfiguredClient, got %T. This is a bug in the tfe provider, so please report it on GitHub.", req.ProviderData),
		)
	}
	r.config = client
}

func (r *resourceWorkspaceRunTask) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version: 0,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Service-generated identifier for the workspace task",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Description: "The id of the workspace to associate the Run task to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"task_id": schema.StringAttribute{
				Description: "The id of the Run task to associate to the Workspace.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"enforcement_level": schema.StringAttribute{
				Description: fmt.Sprintf("The enforcement level of the task. Valid values are %s.", sentenceList(
					workspaceRunTaskEnforcementLevels(),
					"`",
					"`",
					"and",
				)),
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf(workspaceRunTaskEnforcementLevels()...),
				},
			},
			"stage": schema.StringAttribute{
				Description: fmt.Sprintf("The stage to run the task in. Valid values are %s.", sentenceList(
					workspaceRunTaskStages(),
					"`",
					"`",
					"and",
				)),
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(string(tfe.PostPlan)),
				Validators: []validator.String{
					stringvalidator.OneOf(workspaceRunTaskStages()...),
				},
			},
		},
	}
}

func (r *resourceWorkspaceRunTask) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state modelTFEWorkspaceRunTaskV0

	// Read Terraform current state into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	wstaskID := state.ID.ValueString()
	workspaceID := state.WorkspaceID.ValueString()

	tflog.Debug(ctx, "Reading workspace run task")
	wstask, err := r.config.Client.WorkspaceRunTasks.Read(ctx, workspaceID, wstaskID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading Workspace Run Task", "Could not read Workspace Run Task, unexpected error: "+err.Error())
		return
	}

	result := modelFromTFEWorkspaceRunTask(wstask)

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &result)...)
}

func (r *resourceWorkspaceRunTask) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan modelTFEWorkspaceRunTaskV0

	// Read Terraform planned changes into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	taskID := plan.TaskID.ValueString()
	task, err := r.config.Client.RunTasks.Read(ctx, taskID)
	if err != nil {
		resp.Diagnostics.AddError("Error retrieving task", "Could not read Organization Run Task "+taskID+", unexpected error: "+err.Error())
		return
	}

	workspaceID := plan.WorkspaceID.ValueString()
	if _, err := r.config.Client.Workspaces.ReadByID(ctx, workspaceID); err != nil {
		resp.Diagnostics.AddError("Error retrieving workspace", "Could not read Workspace "+workspaceID+", unexpected error: "+err.Error())
		return
	}

	stage := tfe.Stage(plan.Stage.ValueString())
	level := tfe.TaskEnforcementLevel(plan.EnforcementLevel.ValueString())

	options := tfe.WorkspaceRunTaskCreateOptions{
		RunTask:          task,
		EnforcementLevel: level,
		Stage:            &stage,
	}

	tflog.Debug(ctx, fmt.Sprintf("Create task %s in workspace: %s", taskID, workspaceID))
	wstask, err := r.config.Client.WorkspaceRunTasks.Create(ctx, workspaceID, options)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create workspace task", err.Error())
		return
	}

	result := modelFromTFEWorkspaceRunTask(wstask)

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &result)...)
}

func (r *resourceWorkspaceRunTask) stringPointerToStagePointer(val *string) *tfe.Stage {
	if val == nil {
		return nil
	}
	newVal := tfe.Stage(*val)
	return &newVal
}

func (r *resourceWorkspaceRunTask) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan modelTFEWorkspaceRunTaskV0

	// Read Terraform planned changes into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	level := tfe.TaskEnforcementLevel(plan.EnforcementLevel.ValueString())
	stage := r.stringPointerToStagePointer(plan.Stage.ValueStringPointer())

	options := tfe.WorkspaceRunTaskUpdateOptions{
		EnforcementLevel: level,
		Stage:            stage,
	}

	wstaskID := plan.ID.ValueString()
	workspaceID := plan.WorkspaceID.ValueString()

	tflog.Debug(ctx, fmt.Sprintf("Update task %s in workspace %s", wstaskID, workspaceID))
	wstask, err := r.config.Client.WorkspaceRunTasks.Update(ctx, workspaceID, wstaskID, options)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update workspace task", err.Error())
		return
	}

	result := modelFromTFEWorkspaceRunTask(wstask)

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &result)...)
}

func (r *resourceWorkspaceRunTask) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state modelTFEWorkspaceRunTaskV0
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	wstaskID := state.ID.ValueString()
	workspaceID := state.WorkspaceID.ValueString()

	tflog.Debug(ctx, fmt.Sprintf("Delete task %s in workspace %s", wstaskID, workspaceID))
	err := r.config.Client.WorkspaceRunTasks.Delete(ctx, workspaceID, wstaskID)
	// Ignore 404s for delete
	if err != nil && !errors.Is(err, tfe.ErrResourceNotFound) {
		resp.Diagnostics.AddError(
			"Error deleting workspace run task",
			fmt.Sprintf("Couldn't delete task %s in workspace %s: %s", wstaskID, workspaceID, err.Error()),
		)
	}
	// Resource is implicitly deleted from resp.State if diagnostics have no errors.
}

func (r *resourceWorkspaceRunTask) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	s := strings.SplitN(req.ID, "/", 3)
	if len(s) != 3 {
		resp.Diagnostics.AddError(
			"Error importing workspace run task",
			fmt.Sprintf("Invalid task input format: %s (expected <ORGANIZATION>/<WORKSPACE NAME>/<TASK NAME>)", req.ID),
		)
		return
	}

	taskName := s[2]
	workspaceName := s[1]
	orgName := s[0]

	if wstask, err := fetchWorkspaceRunTask(taskName, workspaceName, orgName, r.config.Client); err != nil {
		resp.Diagnostics.AddError(
			"Error importing workspace run task",
			err.Error(),
		)
	} else if wstask == nil {
		resp.Diagnostics.AddError(
			"Error importing workspace run task",
			"Workspace task does not exist or has no details",
		)
	} else {
		result := modelFromTFEWorkspaceRunTask(wstask)
		resp.Diagnostics.Append(resp.State.Set(ctx, &result)...)
	}
}
