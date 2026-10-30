using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;

namespace Cmnet.Native;

// Virtualize rows, not a page-sized subset. Only realized rows own portraits.
internal sealed class HeroCatalog : ListBox
{
    private int[] heroes = [];
    private Func<int, Button>? createTile;
    private int columns = 1;

    public HeroCatalog()
    {
        BorderThickness = new Thickness(0);
        Background = Brushes.Transparent;
        ItemContainerStyle = (Style)Application.Current.FindResource("CompactListItem");
        VirtualizingPanel.SetIsVirtualizing(this, true);
        VirtualizingPanel.SetVirtualizationMode(this, VirtualizationMode.Recycling);
        ScrollViewer.SetCanContentScroll(this, true);
        ScrollViewer.SetHorizontalScrollBarVisibility(this, ScrollBarVisibility.Disabled);
        KeyboardNavigation.SetDirectionalNavigation(this, KeyboardNavigationMode.Contained);
        SizeChanged += (_, _) =>
        {
            var next = Math.Max(1, (int)((ActualWidth - 18) / 75));
            if (next == columns) return;
            columns = next;
            RebuildRows();
        };
    }

    public void SetHeroes(int[] ids, Func<int, Button> factory)
    {
        heroes = ids;
        createTile = factory;
        RebuildRows();
    }

    private void RebuildRows() => ItemsSource = heroes.Chunk(columns).ToArray();

    protected override void PrepareContainerForItemOverride(DependencyObject element, object item)
    {
        base.PrepareContainerForItemOverride(element, item);
        if (element is not ListBoxItem container || item is not int[] ids || createTile == null) return;
        var row = new StackPanel { Orientation = Orientation.Horizontal, Height = 70 };
        foreach (var id in ids) row.Children.Add(createTile(id));
        container.Content = row;
        container.Focusable = false;
    }

    public IEnumerable<Button> VisibleButtons()
    {
        for (var i = 0; i < Items.Count; i++)
            if (ItemContainerGenerator.ContainerFromIndex(i) is ListBoxItem { Content: StackPanel row })
                foreach (var button in row.Children.OfType<Button>()) yield return button;
    }
}
