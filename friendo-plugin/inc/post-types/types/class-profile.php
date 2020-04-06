<?php

namespace Friendo\PostTypes;

require_once FRIENDO_PLUGIN_PATH . 'inc/post-types/class-post-type.php';

class Profile extends PostType
{
    protected function getSlug ()
    {
        return 'profile';
    }

    protected function getSingularName()
    {
        return 'Profile';
    }

    protected function getPluralName()
    {
        return 'Profiles';
    }
}