package githubactions

// Export some parts of the schema at the top level
// we want to be part of the core API.

#Job:  matchN(1, [#Workflow.#normalJob, #Workflow.#reusableWorkflowCallJob])
#Step: #Workflow.#step