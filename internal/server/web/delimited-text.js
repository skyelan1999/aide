'use strict';
// CSV/TSV values stay strings: no formula evaluation or numeric coercion.
window.AideDelimitedText = (() => {
  function parse(text, delimiter) {
    const bom=text.startsWith('\ufeff');
    const input=bom?text.slice(1):text;
    const newline=input.includes('\r\n')?'\r\n':input.includes('\r')?'\r':'\n';
    if(!delimiter){
      const counts={',':0,';':0,'\t':0};let quoted=false;
      for(let i=0;i<input.length;i++){
        const c=input[i];
        if(c==='"'){if(quoted&&input[i+1]==='"')i++;else quoted=!quoted;}
        else if(!quoted){if(c==='\n'||c==='\r')break;if(c in counts)counts[c]++;}
      }
      delimiter=Object.keys(counts).sort((a,b)=>counts[b]-counts[a])[0];
    }
    const rows=[];let row=[],value='',quoted=false,closed=false,ended=false;
    const field=()=>{row.push(value);value='';closed=false;};
    for(let i=0;i<input.length;i++){
      const c=input[i];ended=false;
      if(quoted){
        if(c==='"'){if(input[i+1]==='"'){value+='"';i++;}else{quoted=false;closed=true;}}
        else value+=c;
      }else if(c===delimiter){field();}
      else if(c==='\r'||c==='\n'){
        field();rows.push(row);row=[];if(c==='\r'&&input[i+1]==='\n')i++;ended=true;
      }else if(c==='"'&&!value&&!closed){quoted=true;}
      else{if(closed||c==='"')throw new Error('CSV 引号格式错误，请在原文中修正');value+=c;}
    }
    if(quoted)throw new Error('CSV 引号未闭合，请在原文中修正');
    if(!ended&&(input.length||row.length||value)){field();rows.push(row);}
    return {rows,delimiter,newline,bom,trailingNewline:ended};
  }
  function stringify(data){
    const quote=value=>{
      const s=String(value);
      return s.includes(data.delimiter)||/["\r\n]/.test(s)?'"'+s.replace(/"/g,'""')+'"':s;
    };
    return (data.bom?'\ufeff':'')+data.rows.map(row=>row.map(quote).join(data.delimiter)).join(data.newline)+(data.trailingNewline?data.newline:'');
  }
  return {parse,stringify};
})();
