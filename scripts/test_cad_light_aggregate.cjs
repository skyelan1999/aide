"use strict";

const fs = require("fs"), vm = require("vm"), assert = require("assert/strict"), THREE = require("../internal/server/web/vendor/three.min.js");

const s = fs.readFileSync(process.env.AIDE_URDF_REVIEW_BASELINE || "internal/server/web/urdf-preview.js", "utf8"), a = s.indexOf("    const track=object=>{"), b = s.indexOf("    function teardown()", a);

assert(a >= 0 && b > a);

const c = {
    LIMITS: {
        triangles: 3e6,
        colladaLights: 16
    },
    uiTextUrdfPreview: s => s,
    geometries: new Set,
    materials: new Set,
    textures: new Set,
    objects: new Set,
    importedLights: new Set,
    triangles: 0,
    wireframe: false
};

vm.createContext(c);

vm.runInContext(s.slice(a, b) + "\nthis.ownedTrack=track;", c);

const lights = n => {
    const group = new THREE.Group;
    for (let i = 0; i < n; i++) group.add(new THREE.PointLight);
    return group;
};

const first = lights(8), second = lights(8);

c.ownedTrack(first);

c.ownedTrack(first);

c.ownedTrack(second);

assert.throws((() => c.ownedTrack(lights(1))), /灯光|light/i);

assert.equal(c.importedLights.size, 16, "failed additions do not poison budget");

console.log("PASS aggregate imported-light budget, idempotent tracking and failure atomicity");
