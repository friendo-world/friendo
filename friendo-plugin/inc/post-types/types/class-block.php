<?php

namespace Friendo\PostTypes;

require_once FRIENDO_PLUGIN_PATH . 'inc/post-types/class-post-type.php';

class Block extends PostType
{
    protected function getSlug ()
    {
        return 'block';
    }

    protected function getSingularName()
    {
        return 'Block';
    }

    protected function getPluralName()
    {
        return 'Blocks';
    }
}