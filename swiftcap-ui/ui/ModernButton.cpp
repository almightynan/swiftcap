#include "../ui/ModernButton.h"
#include <wx/dcbuffer.h>
ModernButton::ModernButton(wxWindow* parent, wxWindowID id, const wxString& label, const wxPoint& pos, const wxSize& size, const wxFont& font)
    : wxButton(parent, id, label, pos, size)
{
    SetBackgroundColour(wxColour(0x0f,0x0f,0x0f));
    SetForegroundColour(*wxWHITE);
    SetFont(font);
    SetWindowStyleFlag(GetWindowStyleFlag() | wxBORDER_SIMPLE);
    SetMinSize(size);
    SetMaxSize(size);
    SetCursor(wxCursor(wxCURSOR_HAND));
    Bind(wxEVT_ENTER_WINDOW, &ModernButton::OnEnter, this);
    Bind(wxEVT_LEAVE_WINDOW, &ModernButton::OnLeave, this);
    Bind(wxEVT_PAINT, &ModernButton::OnPaint, this);
    Bind(wxEVT_UPDATE_UI, &ModernButton::OnUpdateUI, this);
}
void ModernButton::OnEnter(wxMouseEvent&) { m_hover = true; Refresh(); }
void ModernButton::OnLeave(wxMouseEvent&) { m_hover = false; Refresh(); }
void ModernButton::OnUpdateUI(wxUpdateUIEvent&) { Refresh(); }
void ModernButton::OnPaint(wxPaintEvent& evt) {
    wxAutoBufferedPaintDC dc(this);
    wxSize sz = GetSize();
    bool disabled = !IsEnabled();
    wxColour bg = disabled ? wxColour(230, 232, 238) : (m_hover ? wxColour(40,40,44) : wxColour(0x0f,0x0f,0x0f));
    wxColour border = disabled ? wxColour(200,200,210) : *wxWHITE;
    dc.SetPen(wxPen(border, 1));
    dc.SetBrush(bg);
    dc.DrawRoundedRectangle(0, 0, sz.GetWidth(), sz.GetHeight(), 8);
    wxString label = GetLabel();
    dc.SetTextForeground(disabled ? wxColour(180,180,200) : *wxWHITE);
    dc.SetFont(GetFont());
    wxCoord tw, th;
    dc.GetTextExtent(label, &tw, &th);
    dc.DrawText(label, (sz.GetWidth()-tw)/2, (sz.GetHeight()-th)/2);
}
