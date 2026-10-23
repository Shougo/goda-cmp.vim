" The Vim side of the "inline" UI (ext/ui/inline). A port of the Vim script of
" ddc-ui-inline (https://github.com/Shougo/ddc-ui-inline, MIT license,
" Copyright Shougo Matsushita). Called only by that UI.

augroup goda_cmp_ui_inline
augroup END

function! goda_cmp#ui#inline#visible() abort
  return s:->get('inline_popup_id', -1) > 0
endfunction

const s:inline_prop_type = 'goda-cmp-ui-inline'
" R: {seq, tick, mode, col, items, params} from the Go side.
function! goda_cmp#ui#inline#show(r) abort
  if !goda_cmp#_fresh(a:r)
    return 0
  endif
  call s:show(a:r.col - 1, a:r.items, a:r.params)
  return 1
endfunction

function! s:show(pos, items, params) abort
  " NOTE: When doing a change motion (i.e. cwabc<esc>) and repeating with ".",
  " it would trigger.

  if mode() ==# 'n'
    return
  endif

  if a:items->empty()
    call goda_cmp#ui#inline#hide()
    return
  endif

  const complete_str = goda_cmp#_input()[a:pos :]
  const next_input = goda_cmp#_next_input()
  const next_word = next_input->matchstr('\w\+$')
  const item_word = a:items[0].word
  const remaining = item_word[complete_str->len():]

  const is_cmdline = mode() ==# 'c'

  const at_eol =
        \   is_cmdline
        \ ? getcmdpos() == getcmdline()->len() + 1
        \ : '.'->col() == '$'->col()

  " Head matched: Follow cursor text
  let head_matched = item_word->stridx(complete_str) == 0
  if a:params.checkNextWordMatched && head_matched && next_word !=# ''
    const next_word_pos = item_word->strridx(next_word)
    let head_matched =
          \ next_word_pos ==# item_word->len() - next_word->len()
  endif

  if head_matched && remaining ==# ''
    call goda_cmp#ui#inline#hide()
    return
  endif

  const word = (head_matched ? remaining : item_word)
        \ ->goda_cmp#ui#inline#_truncate(
        \   a:params.maxWidth, a:params.maxWidth / 3, '...')

  if has('nvim')
    if is_cmdline
      " Use floating window
      let [row, col] = s:get_cmdline_pos(head_matched, at_eol, word)

      let winopts = #{
            \   border: 'none',
            \   relative: 'editor',
            \   width: word->strdisplaywidth(),
            \   height: 1,
            \   row: row,
            \   col: col,
            \   anchor: 'NW',
            \   style: 'minimal',
            \   zindex: 9999,
            \ }

      if goda_cmp#ui#inline#visible()
        " Reuse the window
        call nvim_win_set_config(s:inline_popup_id, winopts)
      else
        " NOTE: It cannot set in nvim_win_set_config()
        let winopts.noautocmd = v:true

        if !'s:inline_popup_buf'->exists()
          const s:inline_popup_buf = nvim_create_buf(v:false, v:true)
        endif

        let s:inline_popup_id = nvim_open_win(
              \ s:inline_popup_buf, v:false, winopts)

        " NOTE: nvim_win_set_option() causes title flicker...
        let highlight = 'Normal:' .. a:params.highlight
        if &hlsearch
          " Disable 'hlsearch' highlight
          let highlight ..= ',Search:None,CurSearch:None'
        endif
        call nvim_win_set_option(
              \ s:inline_popup_id, 'winhighlight', highlight)
        call nvim_win_set_option(
              \ s:inline_popup_id, 'wrap', v:false)
        call nvim_win_set_option(
              \ s:inline_popup_id, 'list', v:false)
        call nvim_win_set_option(
              \ s:inline_popup_id, 'scrolloff', 0)
        call nvim_win_set_option(
              \ s:inline_popup_id, 'statusline', &l:statusline)
      endif

      call nvim_buf_set_lines(s:inline_popup_buf, 0, -1, v:true, [word])
    else
      if 's:namespace'->exists()
        call nvim_buf_clear_namespace(0, s:namespace, 0, -1)
      else
        let s:namespace = nvim_create_namespace('goda_cmp_ui_inline')
      endif

      " Use virtual text
      const col = '.'->col() - 1
      const virt_text_pos =
            \   head_matched && at_eol
            \ ? 'overlay'
            \ : at_eol
            \ ? 'eol'
            \ : 'inline'
      const prefix =
            \   virt_text_pos == 'inline' && word !=# remaining
            \ ? ' '
            \ : ''
      const options = #{
            \   virt_text: [[prefix .. word, a:params.highlight]],
            \   virt_text_pos: virt_text_pos,
            \   hl_mode: 'combine',
            \   priority: 0,
            \   right_gravity: !at_eol,
            \ }
      call nvim_buf_set_extmark(
            \ 0, s:namespace, '.'->line() - 1, col, options)

      " Dummy
      let s:inline_popup_id = 1
    endif
  elseif !is_cmdline && !at_eol
    " Hide the previous popup
    call goda_cmp#ui#inline#hide()

    " Use textprop
    if s:inline_prop_type->prop_type_get(#{ bufnr: bufnr() })->empty()
      call prop_type_add(s:inline_prop_type, #{
            \   bufnr: bufnr(),
            \   highlight: a:params.highlight,
            \ })
    endif

    let row = '.'->line()
    let col =
          \ head_matched
          \ ? '.'->col()
          \ : '.'->col() + 1
    call prop_add(row, col, #{
          \   type: s:inline_prop_type,
          \   bufnr: bufnr(),
          \   text: head_matched ? word : word .. ' ',
          \ })

    " Dummy
    let s:inline_popup_id = 1
  else
    " Use popup window instead
    if is_cmdline
      let [row, col] = s:get_cmdline_pos(head_matched, at_eol, word)
    else
      const spos = screenpos(0, '.'->line(), getcurpos()[2])
      let row = spos.row
      " NOTE: col() does not work for multibyte characters.
      let col =
            \   head_matched
            \ ? goda_cmp#_input()->strwidth() + 2
            \ : goda_cmp#_line()->strwidth() + 3
    endif

    const winopts = #{
          \   pos: 'topleft',
          \   line: row,
          \   col: col,
          \   highlight: a:params.highlight,
          \   zindex: 9999,
          \ }

    if goda_cmp#ui#inline#visible()
      call popup_move(s:inline_popup_id, winopts)
      call popup_settext(s:inline_popup_id, [word])
    else
      let s:inline_popup_id = popup_create([word], winopts)
    endif
  endif

  if is_cmdline
    " NOTE: goda_cmp#hide() does not work here.
    autocmd goda_cmp_ui_inline CmdlineLeave <buffer> ++once call goda_cmp#ui#inline#hide()
    if '##CursorMovedC'->exists()
      autocmd goda_cmp_ui_inline CursorMovedC * ++once ++nested call s:check_cmdline()
    endif

    let s:prev_cmdline = #{
          \   text: getcmdline(),
          \   col: getcmdpos(),
          \ }
  endif
  autocmd goda_cmp_ui_inline ModeChanged <buffer> ++once call goda_cmp#ui#inline#hide()

  redraw
endfunction

function! goda_cmp#ui#inline#hide() abort
  if !goda_cmp#ui#inline#visible()
    return
  endif

  call s:close_popup(s:inline_popup_id)

  let s:inline_popup_id = -1
endfunction
function! s:close_popup(id) abort
  try
    if has('nvim')
      if a:id > 1
        call nvim_win_close(a:id, v:true)
      else
        " Virtual text
        if !'s:namespace'->exists()
          let s:namespace = nvim_create_namespace('goda_cmp_ui_inline')
        endif

        call nvim_buf_clear_namespace(bufnr(), s:namespace, 0, -1)
      endif
    else
      if a:id > 1
        call popup_close(a:id)
      else
        " Clear all properties
        call prop_clear(1, '$'->line())
      endif
    endif

    redraw
  catch /E523:\|E565:\|E5555:\|E994:/
    " Ignore "Not allowed here"

    " Close the popup window later
    call timer_start(100, { -> s:close_popup(a:id) })
  endtry
endfunction

" returns [border_left, border_top, border_right, border_bottom]
function s:get_border_size(border) abort
  if a:border->type() == v:t_string
    return a:border ==# 'none' ? [0, 0, 0, 0] : [1, 1, 1, 1]
  elseif a:border->type() == v:t_list && !a:border->empty()
    return [
          \   s:get_borderchar_width(a:border[3 % len(a:border)]),
          \   s:get_borderchar_height(a:border[1 % len(a:border)]),
          \   s:get_borderchar_width(a:border[7 % len(a:border)]),
          \   s:get_borderchar_height(a:border[5 % len(a:border)]),
          \ ]
  else
    return [0, 0, 0, 0]
  endif
endfunction

function s:get_cmdline_pos(head_matched, at_eol, word) abort
  if '*cmdline#_get'->exists() && !cmdline#_get().pos->empty()
    const [cmdline_left, cmdline_top, cmdline_right, cmdline_bottom]
          \ = s:get_border_size(cmdline#_options().border)

    let pos = cmdline#_get().pos->copy()
    let pos[0] += cmdline_top + cmdline_bottom
    let pos[1] += cmdline_left

    let row = pos[0]
    if has('nvim')
      let row -= 1
    endif

    if !a:at_eol
      if row > 1
        let row -= 1
      else
        let row += 1
      endif
    endif

    const adjustment = [getcmdprompt()->len(), 1]->max()

    let col = pos[1] + 1
    let col += a:head_matched ? getcmdpos() - 1 : getcmdline()->len() + 1
    let col += adjustment
    if !has('nvim') && adjustment ==# 0
      let col += 1
    endif
  else
    let row = &lines - [1, &cmdheight]->max()
    const cmdpos_bytes =
          \   getcmdpos() > 1
          \ ? getcmdline()[: getcmdpos() - 1]->strdisplaywidth()
          \ : getcmdpos()
    let col = cmdpos_bytes + [getcmdprompt()->len(), 1]->max()
    if !a:head_matched
      let col += 1
    endif
    if !a:at_eol
      let col -= 1
    endif

    if !a:at_eol && has('nvim')
      let row -= 1
    endif
  endif

  if !has('nvim')
    let col += 1
  endif

  return [row, col]
endfunction

function! s:check_cmdline() abort
  if s:prev_cmdline.text ==# getcmdline()
        \ && s:prev_cmdline.col !=# getcmdpos()
    call goda_cmp#ui#inline#hide()
  endif
endfunction

function goda_cmp#ui#inline#_truncate(str, max, footer_width, separator) abort
  const width = a:str->strwidth()
  if width <= a:max
    return a:str
  endif

  const header_width = a:max - a:separator->strwidth() - a:footer_width
  const ret = s:strwidthpart(a:str, header_width) .. a:separator
       \ .. s:strwidthpart_reverse(a:str, a:footer_width)
  return s:truncate(ret, a:max)
endfunction
function s:truncate(str, width) abort
  " Original function is from mattn.
  " http://github.com/mattn/googlereader-vim/tree/master

  if a:str =~# '^[\x00-\x7f]*$'
    return a:str->len() < a:width
          \ ? printf('%-' .. a:width .. 's', a:str)
          \ : a:str->strpart(0, a:width)
  endif

  let ret = a:str
  let width = a:str->strwidth()
  if width > a:width
    let ret = s:strwidthpart(ret, a:width)
    let width = ret->strwidth()
  endif

  return ret
endfunction
function s:strwidthpart(str, width) abort
  const str = a:str->tr("\t", ' ')
  const vcol = a:width + 2
  return str->matchstr('.*\%<' .. (vcol < 0 ? 0 : vcol) .. 'v')
endfunction
function s:strwidthpart_reverse(str, width) abort
  const str = a:str->tr("\t", ' ')
  const vcol = str->strwidth() - a:width
  return str->matchstr('\%>' .. (vcol < 0 ? 0 : vcol) .. 'v.*')
endfunction
