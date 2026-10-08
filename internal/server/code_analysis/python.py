# AST only: no execution, imports, plugins, or workspace filesystem access.
import ast,json,sys
out=[]
for f in json.load(sys.stdin):
 u=dict(file=f['id'],language='Python',package='',symbols=[],calls=[],imports=[],diagnostics=[])
 try:
  tree=ast.parse(f['text']);lines=f['text'].splitlines();serial=[0]
  def walk(n,scope=''):
   nextscope=scope
   if isinstance(n,(ast.FunctionDef,ast.AsyncFunctionDef,ast.ClassDef,ast.Lambda)):
    serial[0]+=1
    name=getattr(n,'name',f'anonymous@{n.lineno}:{serial[0]}');key=(scope+'.' if scope else '')+name
    kind='class' if isinstance(n,ast.ClassDef) else 'async function' if isinstance(n,ast.AsyncFunctionDef) else 'function'
    u['symbols'].append(dict(key=key,parent=scope,name=name,kind=kind,line=n.lineno,endLine=getattr(n,'end_lineno',n.lineno),signature=lines[n.lineno-1][:240],anonymous=isinstance(n,ast.Lambda)))
    nextscope=key
   if isinstance(n,ast.Import):
    for a in n.names:u['imports'].append(dict(alias=a.asname or a.name.split('.')[0],path=a.name,name='*'))
   elif isinstance(n,ast.ImportFrom):
    for a in n.names:u['imports'].append(dict(alias=a.asname or a.name,path='.'*n.level+(n.module or ''),name=a.name))
   if isinstance(n,ast.Call):
    c=n.func;name='<dynamic>';qualifier='';dynamic=True
    if isinstance(c,ast.Name):name=c.id;dynamic=False
    elif isinstance(c,ast.Attribute):name=c.attr;qualifier=c.value.id if isinstance(c.value,ast.Name) else '<expression>'
    u['calls'].append(dict(caller=scope,name=name,qualifier=qualifier,line=n.lineno,dynamic=dynamic))
   for c in ast.iter_child_nodes(n):walk(c,nextscope)
  walk(tree)
 except Exception as e:u['diagnostics'].append('语法解析失败：'+str(e)[:180])
 out.append(u)
json.dump(out,sys.stdout,ensure_ascii=False)
