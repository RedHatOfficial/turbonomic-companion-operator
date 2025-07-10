# scripts

## list-triggerless-dcs.py

Lists DeploymentConfigs which do not have triggers defined. When Turbonomic updates such workloads, the changes are not automatically rolled out and rightsizing is not effective. This script can be used to force-roll such workloads.

## remove-override-from-ns.sh

Remove `urbo.ibm.com/override` annotations from all workloads in a namespace, allowing the owner to manage compute resources directly. Perform this step **after** the namespace was removed from Turbonomic rightsizing, otherwise the annotations will be restored.
