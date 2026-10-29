/**
 * Copyright (c) 2006-2015, JGraph Holdings Ltd
 * Copyright (c) 2006-2015, draw.io AG
 *
 * Apache-2.0; see internal/server/web/vendor/drawio/LICENSE.
 * Historical small-graph traversal oracle from Aide dbd06b6f mxGraph.js.
 * This recursive implementation is only used on bounded test graphs; runtime
 * and deep-graph tests use the current iterative implementation.
 */
mxGraph.prototype.traverse = function(vertex, directed, func, edge, visited, inverse)
{
	if (func != null && vertex != null)
	{
		directed = (directed != null) ? directed : true;
		inverse = (inverse != null) ? inverse : false;
		visited = visited || new mxDictionary();

		if (edge == null || !visited.get(edge))
		{
			visited.put(edge, true);
			var result = func(vertex, edge);

			if (result == null || result)
			{
				var edgeCount = this.model.getEdgeCount(vertex);

				if (edgeCount > 0)
				{
					for (var i = 0; i < edgeCount; i++)
					{
						var e = this.model.getEdgeAt(vertex, i);
						var isSource = this.model.getTerminal(e, true) == vertex;

						if (!directed || (!inverse == isSource))
						{
							var next = this.model.getTerminal(e, !isSource);
							this.traverse(next, directed, func, e, visited, inverse);
						}
					}
				}
			}
		}
	}
}
