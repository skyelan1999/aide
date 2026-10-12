/* STEP parsing stays in a dedicated worker so B-rep triangulation cannot freeze the workbench. */
importScripts('/vendor/occt-import-js/occt-import-js.js');

self.onmessage = async (event) => {
  try {
    const occt = await self.occtimportjs({
      locateFile: (path) => new URL('/vendor/occt-import-js/' + path, self.location.origin).href,
    });
    const result = occt.ReadFile('step', new Uint8Array(event.data.buffer), {
      linearDeflectionType: 'bounding_box_ratio',
      linearDeflection: 0.003,
      angularDeflection: 0.45,
    });
    if (!result?.success || !Array.isArray(result.meshes)) {
      self.postMessage({ error: 'STEP 文件无法解析，请确认文件完整且格式有效。' });
      return;
    }
    let triangleCount = 0;
    const transfers = [];
    for (const mesh of result.meshes) {
      const position = mesh.attributes?.position?.array;
      const normal = mesh.attributes?.normal?.array;
      const indices = mesh.index?.array;
      if (!position || !indices) continue;
      triangleCount += indices.length / 3;
      if (triangleCount > 3000000) {
        self.postMessage({ error: '模型三角面超过 300 万上限。请使用较低精度或更小的 STEP 文件。' });
        return;
      }
      mesh.attributes.position.array = Float32Array.from(position);
      mesh.index.array = Uint32Array.from(indices);
      transfers.push(mesh.attributes.position.array.buffer, mesh.index.array.buffer);
      if (normal) {
        mesh.attributes.normal.array = Float32Array.from(normal);
        transfers.push(mesh.attributes.normal.array.buffer);
      }
    }
    self.postMessage({ result }, transfers);
  } catch (error) {
    self.postMessage({ error: error?.message || 'STEP 解析失败。' });
  }
};
