'use strict';
// Selected legacy/current AWS palettes from issue185. Shared with the scoped
// bundle synchronizer; top-level initializers supply the actual style args.
const current=['GeneralResources','Illustrations','Compute','InternetOfThings','ManagementGovernance','Storage'];
const legacy=['GeneralResources','Illustrations','Database','NetworkContentDelivery'];
const methods=[...current.map(n=>['diagramly/sidebar/Sidebar-AWS4.js','Sidebar.prototype.addAWS4'+n+'Palette']),
  ...legacy.map(n=>['diagramly/sidebar/Sidebar-AWS4b.js','Sidebar.prototype.addAWS4b'+n+'Palette'])];
const cases=[
 ['current','json_script','json script',['internet','gateway']],
 ['current','illustration_notification','notification',['users']],
 ['current','connector','connector',['notebook']],
 ['current','ec2_r6a_instance','r6a',['r5a']],
 ['current','ec2_x2iedn_instance','x2iedn',['x2eidn']],
 ['current','ec2_x2iezn_instance','x2iezn',['x2eizn']],
 ['current','systems_manager_compliance','systems manager compliance',['scompliance']],
 ['current','systems_manager_distributor','systems manager distributor',['scompliance','compliance']],
 ['current','archive','archive',[],'Archive'],
 ['current','vault','vault',[],'Vault'],
 ['legacy','illustration_notification','notification',['users']],
 ['legacy','generic_database','generic database',['databas']],
 ['legacy','postgresql_instance','postgresql instance',['postresql']],
 ['legacy','vpc','network',['netowrk']]
];
module.exports={methods,cases};
