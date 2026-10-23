" The Vim side of ext/source/vim: Vim script completion. A port of the Vim
" script of ddc-source-vim (https://github.com/Shougo/ddc-source-vim, MIT
" license, Copyright Shougo Matsushita). Called only by that source.

function goda_cmp#source#vim#get_cur_text(input) abort
  if mode() ==# 'c'
    return a:input
  endif

  let cur_text = a:input

  let line = '.'->line()
  let cnt = 0
  const cursor_text_pattern =
        \ '^\s*[\\\(\[,\|].*\%(' ..
        \ '<\%(let\|const\|for\|var\|final\|fu\%[nction]\|def\)\>\|=' ..
        \ '\)'
  while line > 1 && cnt < 10
    if cur_text !~# '^[\s\\]*$' && cur_text !~# cursor_text_pattern
      break
    endif

    let prev_line = (line - 1)->getline()
    if prev_line =~# '\S' || cur_text =~# '^\s*\\'
      let cur_text = prev_line .. cur_text->substitute('^\s*\\', '', '')
    else
      break
    endif

    let line -= 1
    let cnt += 1
  endwhile

  return cur_text->split('\s\+|\s\+\|<bar>', 1)[-1]
endfunction

function s:get_completion_type(input) abort
  if '*getcompletiontype'->exists()
    return getcompletiontype(a:input)
  endif

  " Fallback.
  if a:input =~ '^\w*map\s'
    return 'mapping'
  elseif a:input =~ '\$\w*$'
    return 'environment'
  endif

  return ''
endfunction

function goda_cmp#source#vim#gather(input, complete_str) abort
  const cur_text = a:input->goda_cmp#source#vim#get_cur_text()
  const completion_type = s:get_completion_type(cur_text)

  " TODO: Use getcmdcompltype() to parse Vim script.
  if completion_type ==# 'option' || a:complete_str =~# '^&\%([gl]:\?\)\?'
    " Options.
    const prefix = a:complete_str->matchstr('^&\%([gl]:\?\)\?')
    let list = cur_text->goda_cmp#source#vim#option()->deepcopy()
    for keyword in list
      let keyword.word = prefix .. keyword.word
    endfor
    return list
  elseif cur_text =~# '\<has($\?[''"]\w*$'
    " Features.
    return goda_cmp#source#vim#feature()
  elseif cur_text =~# '\<expand($\?[''"][<>[:alnum:]]*$'
    " Expand.
    return goda_cmp#source#vim#expand()
  elseif cur_text =~# '\$["''].*{\zs[^}]*$\|->\w*$'
    " Expression.
    return a:complete_str
          \ ->getcompletion('expression')
          \ ->s:make_completion_list()
  elseif completion_type ==# 'mapping'
    " Maps.
    return goda_cmp#source#vim#map()
  elseif completion_type ==# 'environment'
    " Environment.
    return goda_cmp#source#vim#environment()
  elseif cur_text !~# '^[[:digit:],[:space:][:tab:]$''<>]*\h\w*$'
    return s:get_local_variables()
  endif

  return []
endfunction

function goda_cmp#source#vim#option(cur_text) abort
  if a:cur_text =~# '\<set\%[local]\s\+\%(filetype\|ft\)='
    return goda_cmp#source#vim#filetype()
  endif

  if !exists('s:options')
    let s:options = s:make_cache_options()
  endif

  return s:options
endfunction

function goda_cmp#source#vim#map() abort
  if !exists('s:maps')
    let s:maps = s:make_cache_maps() + s:make_cache_keys()
  endif

  return s:maps
endfunction

function goda_cmp#source#vim#feature() abort
  return [
        \   #{
        \     word: 'patch',
        \     menu: '; Included patches Ex: patch123',
        \   },
        \   #{
        \     word: 'patch-',
        \     menu: '; Version and patches Ex: patch-7.4.237',
        \   },
        \ ]
endfunction

function goda_cmp#source#vim#expand() abort
  return [
        \   ':e',
        \   ':h',
        \   ':p',
        \   ':r',
        \   ':t',
        \   '<SID>',
        \   '<abuf>',
        \   '<afile>',
        \   '<amatch>',
        \   '<cWORD>',
        \   '<cexpr>',
        \   '<cfile>',
        \   '<client>',
        \   '<cword>',
        \   '<script>',
        \   '<sfile>',
        \   '<sflnum>',
        \   '<slnum>',
        \   '<stack>',
        \ ]->s:make_completion_list()
endfunction

function goda_cmp#source#vim#environment() abort
  return environ()->keys()->map({
        \ _, val ->
        \  #{
        \     word: '$' .. val,
        \     kind: 'e',
        \   }
        \ })
endfunction

function goda_cmp#source#vim#filetype() abort
  if !exists('s:filetypes')
    const globs =
          \ 'syntax/*.vim'->globpath(&runtimepath, v:true, v:true) +
          \ 'indent/*.vim'->globpath(&runtimepath, v:true, v:true) +
          \ 'ftplugin/*.vim'->globpath(&runtimepath, v:true, v:true)

    let s:filetypes =
          \ globs->map({
          \   _, val ->
          \   val->fnamemodify(':t:r')->matchstr('^[[:alnum:]-]*')
          \ })->s:make_completion_list()
  endif

  return s:filetypes
endfunction

function s:get_local_variables() abort
  let keyword_dict = {}
  let line_num = '.'->line() - 1
  let function_start = 0
  while line_num >= 1
    let line = line_num->getline()
    if line =~# '\<endf\%[unction]\>'
      break
    elseif line =~# '\<\%(fu\%[nction]\|def\)!\?\s\+'
      let function_start = line_num
      call s:analyze_variable_line(line, keyword_dict)
      break
    endif

    let line_num -= 1
  endwhile

  if function_start == 0
    return keyword_dict->values()
  endif

  let line_num = function_start + 1
  let end_line = '.'->line() - 1
  while line_num <= end_line
    let line = line_num->getline()
    if line =~# '\<\%(let\|const\|for\|var\|final\)\s\+'
      call s:analyze_variable_line(line, keyword_dict)
    elseif line =~# '\<\%(fu\%[nction]\|def\)!\?\s\+'
      " Skip nested definitions conservatively.
      let nested_line = line_num
      while nested_line <= end_line
        if nested_line->getline() =~# '\<endf\%[unction]\>'
          let line_num = nested_line
          break
        endif
        let nested_line += 1
      endwhile
    endif

    let line_num += 1
  endwhile

  return keyword_dict->values()
endfunction

function s:extract_kinds(expression) abort
  let kinds = []
  if a:expression =~# '^\%\(\s*+\)\?\s*\d\+\.\d\+'
    call add(kinds, '.')
  endif
  if a:expression =~# '^\%\(\s*+\)\?\s*\d\+'
    call add(kinds, '0')
  endif
  if a:expression =~# '^\%\(\s*\.\)\?\s*["'']'
    call add(kinds, '""')
  endif
  if a:expression =~# '\<function('
    call add(kinds, '()')
  endif
  if a:expression =~# '^\%\(\s*+\)\?\s*\['
    call add(kinds, '[]')
  endif
  if a:expression =~# '^\s*{\|^\.\h[[:alnum:]_:]*'
    call add(kinds, '{}')
  endif
  return kinds
endfunction

function s:pick_primary_kind(kinds) abort
  return a:kinds->empty() ? '' : a:kinds[0]
endfunction

function s:analyze_variable_line(line, keyword_dict) abort
  let cur_line = a:line
  let continued = cur_line =~# '\\$'
  if continued
    let cur_line = cur_line->substitute('\\\s*$', '', '')
  endif

  let matched_single_var = cur_line->matchlist(
        \ '\<\%(let\|const\|for\|var\|final\)\s\+' ..
        \ '\(\a[[:alnum:]_:]*\)\%([^=]*=\s*\(.*\)\)\?')
  if !matched_single_var->empty()
    let word = matched_single_var[1]
    let expression = matched_single_var[2]
    let kinds = s:extract_kinds(expression)
    if !has_key(a:keyword_dict, word)
      let a:keyword_dict[word] = #{
            \    word: word,
            \    kind: s:pick_primary_kind(kinds),
            \    kinds: kinds,
            \ }
    elseif expression !=# ''
      if a:keyword_dict[word]->get('kind', '') ==# ''
        let a:keyword_dict[word].kind = s:pick_primary_kind(kinds)
      endif
      if !has_key(a:keyword_dict[word], 'kinds') || a:keyword_dict[word].kinds->empty()
        let a:keyword_dict[word].kinds = kinds
      else
        for kind in kinds
          if index(a:keyword_dict[word].kinds, kind) < 0
            call add(a:keyword_dict[word].kinds, kind)
          endif
        endfor
      endif
    endif

    return
  endif

  let matched_multiple_vars = cur_line->matchlist(
        \ '\<\%(let\|const\)\s\+\[\(.*\)\]\s*=\s*')
  if !matched_multiple_vars->empty()
    for word in matched_multiple_vars[1]->split('\s*,\s*')
      let a:keyword_dict[word] = #{
            \   word: word,
            \ }
    endfor

    return
  endif

  if cur_line =~# '\<fu\%[nction]!\?\s\+'
    let arg_text = cur_line->matchstr('^[^(]*(\zs[^)]*')
    if continued && arg_text !~# ')'
      let arg_text = arg_text .. ' '
    endif
    for arg in arg_text->split('\s*,\s*')
      let word = 'a:' .. (arg ==# '...' ? '000' : arg->matchstr('\w\+'))
      let arg_kind = (arg ==# '...' ? '[]' : '')
      let a:keyword_dict[word] = #{
            \    word: word,
            \    kind: arg_kind,
            \    kinds: arg_kind ==# '' ? [] : [arg_kind],
            \ }

    endfor
    if cur_line =~# '\.\.\.)'
      for arg in 5->range()
        let word = 'a:' .. arg
        let a:keyword_dict[word] = #{
              \   word: word,
              \   kind: (arg == 0 ? '0' : ''),
              \   kinds: (arg == 0 ? ['0'] : []),
              \ }
      endfor
    endif
  elseif cur_line =~# '\<def!\?\s\+'
    let arg_text = cur_line->matchstr('^[^(]*(\zs[^)]*')
    if continued && arg_text !~# ')'
      let arg_text = arg_text .. ' '
    endif
    for arg in arg_text->split('\s*,\s*')
      let word = arg->matchstr('\w\+')
      let a:keyword_dict[word] = #{
            \    word: word,
            \    kinds: [],
            \ }

    endfor
  endif
endfunction

function s:get_variable_type(expression) abort
  return s:pick_primary_kind(s:extract_kinds(a:expression))
endfunction

function s:make_cache_options() abort
  let options =
        \ 'set all'->execute()->split('\s\{2,}\|\n')[1:]
        \ ->filter({ _, val -> !val->empty() && val =~# '^\h\w*=\?' })
        \ ->map({ _, val -> val->substitute('^no\|=\zs.*$', '', '') })

  for option in options->copy()
    if option[-1:] !=# '='
      call add(options, 'no' .. option)
    endif
  endfor

  return options
        \ ->filter({ _, val -> val =~# '^\h\w*=\?' })
        \ ->map({
        \   _, val ->
        \   #{ word: val->substitute('=$', '', ''), kind: 'o' }
        \ })
endfunction

function s:make_cache_maps() abort
  const helpfile = 'doc/map.txt'->findfile(&runtimepath)->expand()
  if !helpfile->filereadable()
    return []
  endif

  const lines = helpfile->readfile()
  const start = lines->match('1\. Key mapping')
  const end = lines->match('2\. Abbreviations')

  return lines[start : end]->map({
        \   _, val -> val->matchstr('\*\%(:map-\)\?\zs\(<\k\+>\)\ze\*')
        \ })->filter({ _, val -> val !=# ''})->s:make_completion_list()
endfunction

function s:make_cache_keys() abort
  const helpfile = 'doc/intro.txt'->findfile(&runtimepath)->expand()
  if !helpfile->filereadable()
    return []
  endif

  const lines = helpfile->readfile()
  const start = lines->match('\*key-notation')
  const end = lines->match('\*vim-modes-intro\*')

  let keys = [
        \   '<LeftMouse>', '<LeftDrag>', '<LeftRelease>',
        \   '<RightMouse>', '<RightDrag>', '<RightRelease>',
        \   '<MiddleMouse>', '<MiddleDrag>', '<MiddleRelease>',
        \   '<X1Mouse>', '<X1Drag>', '<X1Release>',
        \   '<X2Mouse>', '<X2Drag>', '<X2Release>',
        \   '<2-LeftMouse>', '<3-LeftMouse>', '<4-LeftMouse>',
        \   '<A-LeftMouse>', '<S-LeftMouse>', '<C-LeftMouse>',
        \   '<A-RightMouse>', '<S-RightMouse>', '<C-RightMouse>',
        \ ] + lines[start : end]->map({
        \   _, val -> val->matchstr('<\k\+>')->substitute('-…>', '-', '')
        \ })->filter({ _, val -> val !=# ''})

  return keys->s:make_completion_list()
endfunction

function s:make_completion_list(list) abort
  return a:list->uniq()->map({
        \ _, val ->
        \   val !=# '' && val[-1:] ==# '/' ?
        \   #{
        \     word: val[:-2],
        \     abbr: val,
        \   } : #{
        \     word: val,
        \   }
        \ })
endfunction
