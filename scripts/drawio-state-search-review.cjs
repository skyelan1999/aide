'use strict';
const methods=[
 ['diagramly/sidebar/Sidebar-Android.js','Sidebar.prototype.addAndroidPalette'],
 ['diagramly/sidebar/Sidebar-ArchiMate.js','Sidebar.prototype.addArchiMatePalette'],
 ...['Application','Business','Technology'].map(n=>['diagramly/sidebar/Sidebar-ArchiMate3.js','Sidebar.prototype.addArchimate3'+n+'Palette']),
 ['diagramly/sidebar/Sidebar-Arrows2.js','Sidebar.prototype.addArrows2Palette'],
 ['diagramly/sidebar/Sidebar-Atlassian.js','Sidebar.prototype.addAtlassianPalette']
];
const initializers=[['diagramly/sidebar/Sidebar-ArchiMate3.js','Sidebar.prototype.addArchimate3Palette']];
// Required explicit tags, not the title terms automatically added by Sidebar.
const targets=[
 [192,'archimate2','Infrastructure Function',1,['infrastructure','function'],['infraastructure']],
 [192,'archimate2','Assessment',1,['assessment'],['assesment']],
 [192,'archimate2','Requirement',1,['requirement'],['goal']],
 [193,'archimate3','Application Interface',1,['interface'],['component']],
 [193,'archimate3','Data Object',2,['data','object'],['service']],
 [193,'archimate3','Business Object',2,['business','object'],['service','data']],
 [193,'archimate3','Representation',2,['representation'],['contract']],
 [193,'archimate3','System Software',2,['system','software'],['device']],
 [193,'archimate3','Technology Interface',1,['interface'],['component']],
 [193,'archimate3','Equipment',2,['equipment'],['artifact']],
 [194,'arrows2','Callout with Quad Arrow',1,['four','way'],['war']],
 [195,'atlassian','Checkbox (off)',1,['checkbox','off'],['on']]
];
module.exports={methods,initializers,targets};
