'use strict';
const path = require('path');
const parameterProperties = {
  diameter_mm: {type:'number', minimum:30, maximum:200, description:'Default 83.1 mm (3340S official approximate diameter)'},
  pitch_mm: {type:'number', minimum:10, maximum:300, description:'Default 101.6 mm (nominal 4 inch pitch, not measured local twist)'},
  blades: {type:'integer', minimum:2, maximum:6, description:'Default 4, visually confirmed on official product photo'},
  hub_radius_mm: {type:'number',minimum:2,maximum:20,description:'Assumed aerodynamic cutout; default 7 mm, NOT an original hub dimension'},
  chord_scale: {type:'number',minimum:.6,maximum:1.4},
  pitch_scale: {type:'number',minimum:.6,maximum:1.4},
  lift_slope_scale: {type:'number',minimum:.6,maximum:1.4},
  mass_g: {type:'number',minimum:100,maximum:2000,description:'Total vehicle mass, default 455 g'},
  rotors: {type:'integer',minimum:1,maximum:8,description:'Default 4. Single rotor loads = vehicle weight / rotors'},
  altitude_m: {type:'number',minimum:0,maximum:6000},
  axial_speed_ms: {type:'number',minimum:0,maximum:15,description:'Positive axial inflow. Not a maximum climb prediction'},
};
function outputDirectory(value) {
  const text = String(value || 'avata360_3340s_reconstruction');
  if (!/^[A-Za-z0-9_-]+(?:\/[A-Za-z0-9_-]+)*$/.test(text) || text.length>160) throw new Error('Use a relative output directory with letters, numbers, underscores or hyphens');
  if (text.split('/').some(x=>['plugins','scripts','node_modules'].includes(x))) throw new Error('Do not generate into program directories');
  return text;
}
module.exports = {
  name:'propeller-aero',
  apply(ctx) {
    ctx.tool({name:'propeller_analyze',description:'桨叶气动计算 / Propeller BEMT: deterministic axial BEMT and sensitivity; defaults to DJI Avata 360 3340S official diameter/pitch with assumed sections. Read-only, no files written. NOT CFD, no absolute noise/endurance prediction.',parameters:{type:'object',properties:parameterProperties},
      async handler(args,api) { return JSON.parse(await api.runPython('worker.py',{...args,action:'analyze'},{timeoutMs:60000})); }});
    ctx.tool({name:'propeller_reconstruct',description:'桨叶三维重建与图表 / Create four closed blade solids, mirrored STL, SVG/HTML preview, BEMT CSV and provenance report. Hub dimensions unavailable. Returns file proposals ONLY; user application required before claiming files exist.',parameters:{type:'object',properties:{...parameterProperties,output_dir:{type:'string',description:'Fresh relative output directory; do not overwrite existing originals'}}},
      async handler(args,api) {
        const directory=outputDirectory(args.output_dir);
        const result=JSON.parse(await api.runPython('worker.py',{...args,action:'artifacts'},{timeoutMs:60000}));
        const outputs=[{text:JSON.stringify({status:'proposed_not_written',directory,operating_point:result.operating_point,assumptions:result.summary.assumptions,files:Object.keys(result.files)})}];
        for (const [name,content] of Object.entries(result.files)) {
          if (path.basename(name)!==name) throw new Error('Invalid artifact name');
          outputs.push(api.proposeWrite(directory+'/'+name,content));
        }
        return outputs;
      }});
  }
};
