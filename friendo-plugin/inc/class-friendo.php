<?php

namespace Friendo;

require_once FRIENDO_PLUGIN_PATH . 'inc/post-types/class-post-types.php';
require_once FRIENDO_PLUGIN_PATH . 'inc/admin/class-admin.php';
require_once FRIENDO_PLUGIN_PATH . 'inc/frontend/class-frontend.php';
require_once FRIENDO_PLUGIN_PATH . 'inc/homepage/class-homepage.php';

use Friendo\PostTypes;
use Friendo\Admin;
use Friendo\Frontend;
use Friendo\Homepage;

class Friendo
{
    final public function init ()
    {
        $friendo = new self;

        $friendo->loadPostTypes();
        $friendo->loadHomepage();

        if (is_admin()) {
            $friendo->loadAdmin();
        } else {
            $friendo->loadFrontend();
        }
    }

    protected function loadAdmin ()
    {
        Admin::init();
    }

    protected function loadPostTypes ()
    {
        PostTypes::init();
    }

    protected function loadFrontend ()
    {
        Frontend::init();
    }

    protected function loadHomepage ()
    {
        Homepage::init();
    }
}